import io
import os
from pathlib import Path
import stat
import tarfile
import tempfile
import unittest
import zipfile

from release_archive import create_archive, verify_archive, verify_extraction


class ReleaseArchiveTests(unittest.TestCase):
    binary_name = "gosub_linux_amd64"

    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix="gosub-archive-test-")
        self.addCleanup(self.temporary.cleanup)
        self.archive_path = Path(self.temporary.name) / "release.tar.gz"

    def members(self):
        result = []
        for name, mode in ((self.binary_name, 0o755), ("config.example.json", 0o644), ("README.md", 0o644)):
            member = tarfile.TarInfo(name)
            member.mode = mode
            member.size = 4
            result.append(member)
        return result

    def write_tar(self, members):
        with tarfile.open(self.archive_path, "w:gz", format=tarfile.USTAR_FORMAT) as archive:
            for member in members:
                content = io.BytesIO(b"test") if member.isreg() else None
                archive.addfile(member, content)

    def test_safe_tar_archive(self):
        self.write_tar(self.members())
        verify_archive(self.archive_path, self.binary_name)

    def test_creates_all_platform_archives_with_explicit_metadata(self):
        package = Path(self.temporary.name) / "package"
        package.mkdir(mode=0o700)
        (package / "config.example.json").write_bytes(b"example")
        (package / "README.md").write_bytes(b"readme")
        (package / "unexpected.json").write_bytes(b"excluded extra file")
        (package / "config.json").write_bytes(b"private configuration")
        (package / "gosub.db").write_bytes(b"private database")
        for system in ("linux", "darwin", "windows"):
            for architecture in ("amd64", "arm64"):
                with self.subTest(system=system, architecture=architecture):
                    binary = f"gosub_{system}_{architecture}"
                    suffix = ".zip" if system == "windows" else ".tar.gz"
                    if system == "windows":
                        binary += ".exe"
                    (package / binary).write_bytes(b"binary")
                    archive = Path(self.temporary.name) / (system + architecture + suffix)
                    create_archive(archive, binary, package)
                    verify_archive(archive, binary)
                    if suffix == ".zip":
                        with zipfile.ZipFile(archive) as packed:
                            names = packed.namelist()
                    else:
                        with tarfile.open(archive, "r:gz") as packed:
                            names = packed.getnames()
                    self.assertEqual(set(names), {binary, "config.example.json", "README.md"})
                    self.assertEqual(len(names), 3)

    def test_refuses_source_directory(self):
        package = Path(self.temporary.name) / "package"
        (package / self.binary_name).mkdir(parents=True)
        with self.assertRaisesRegex(ValueError, "regular file"):
            create_archive(self.archive_path, self.binary_name, package)

    def test_rejects_original_directory_metadata_bug(self):
        directory = tarfile.TarInfo("./")
        directory.type = tarfile.DIRTYPE
        directory.mode = 0o755
        directory.uid = 1001
        directory.gid = 1234
        self.write_tar([directory] + self.members())
        with self.assertRaisesRegex(ValueError, "exactly"):
            verify_archive(self.archive_path, self.binary_name)

    def test_rejects_unsafe_or_unexpected_paths(self):
        for name in (".", "../escape", "/absolute", "nested/file", "config.json", "gosub.db", "unexpected.json"):
            with self.subTest(name=name):
                members = self.members()
                members[0].name = name
                self.write_tar(members)
                with self.assertRaises(ValueError):
                    verify_archive(self.archive_path, self.binary_name)

    def test_rejects_links_and_directories(self):
        for kind in (tarfile.SYMTYPE, tarfile.LNKTYPE, tarfile.DIRTYPE):
            with self.subTest(kind=kind):
                members = self.members()
                members[0].type = kind
                members[0].linkname = "/root"
                self.write_tar(members)
                with self.assertRaisesRegex(ValueError, "regular file"):
                    verify_archive(self.archive_path, self.binary_name)

    def test_rejects_builder_ownership(self):
        for attribute, value in (("uid", 1001), ("gid", 1234), ("uname", "runner"), ("gname", "runner")):
            with self.subTest(attribute=attribute):
                members = self.members()
                setattr(members[0], attribute, value)
                self.write_tar(members)
                with self.assertRaisesRegex(ValueError, "ownership"):
                    verify_archive(self.archive_path, self.binary_name)

    def test_rejects_pax_extended_metadata(self):
        members = self.members()
        members[0].pax_headers = {"SCHILY.acl.access": "user::rwx,group::rwx,other::rwx"}
        with tarfile.open(self.archive_path, "w:gz", format=tarfile.PAX_FORMAT) as archive:
            for member in members:
                archive.addfile(member, io.BytesIO(b"test"))
        with self.assertRaisesRegex(ValueError, "extra metadata"):
            verify_archive(self.archive_path, self.binary_name)

    def test_rejects_unsafe_permissions(self):
        for index, mode in ((0, 0o4755), (0, 0o777), (0, 0o644), (1, 0o666)):
            with self.subTest(index=index, mode=mode):
                members = self.members()
                members[index].mode = mode
                self.write_tar(members)
                with self.assertRaisesRegex(ValueError, "permissions"):
                    verify_archive(self.archive_path, self.binary_name)

    def test_rejects_duplicate_members(self):
        members = self.members()
        members[1] = members[0]
        self.write_tar(members)
        with self.assertRaises(ValueError):
            verify_archive(self.archive_path, self.binary_name)

    def test_rejects_extra_file(self):
        members = self.members()
        extra = tarfile.TarInfo("unexpected.json")
        extra.size = 4
        extra.mode = 0o644
        self.write_tar(members + [extra])
        with self.assertRaisesRegex(ValueError, "exactly"):
            verify_archive(self.archive_path, self.binary_name)

    def write_zip(self, unsafe=False):
        self.archive_path = self.archive_path.with_suffix(".zip")
        with zipfile.ZipFile(self.archive_path, "w", zipfile.ZIP_DEFLATED) as archive:
            for member in self.members():
                item = zipfile.ZipInfo(member.name)
                item.create_system = 3
                item.external_attr = (stat.S_IFREG | member.mode) << 16
                if unsafe and member.name == self.binary_name:
                    item.external_attr = (stat.S_IFLNK | 0o777) << 16
                archive.writestr(item, b"test")

    def test_safe_zip_archive(self):
        self.write_zip()
        verify_archive(self.archive_path, self.binary_name)

    def test_rejects_zip_symlink(self):
        self.write_zip(unsafe=True)
        with self.assertRaisesRegex(ValueError, "regular file"):
            verify_archive(self.archive_path, self.binary_name)

    @unittest.skipUnless(os.name == "posix", "POSIX directory metadata requires Linux")
    def test_detects_directory_permission_change_during_extraction(self):
        directory = tarfile.TarInfo("./")
        directory.type = tarfile.DIRTYPE
        directory.mode = 0o755
        directory.uid = os.getuid()
        directory.gid = os.getgid()
        self.write_tar([directory] + self.members())
        with self.assertRaisesRegex(ValueError, "directory permissions or ownership"):
            verify_extraction(self.archive_path)

    @unittest.skipUnless(os.name == "posix", "POSIX directory metadata requires Linux")
    def test_extraction_preserves_directory_and_runtime_data(self):
        self.write_tar(self.members())
        verify_archive(self.archive_path, self.binary_name)
        self.assertTrue(verify_extraction(self.archive_path))
        self.write_zip()
        verify_archive(self.archive_path, self.binary_name)
        self.assertTrue(verify_extraction(self.archive_path))


if __name__ == "__main__":
    unittest.main()
