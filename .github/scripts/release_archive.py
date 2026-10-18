import argparse
import os
from pathlib import Path
import stat
import subprocess
import tarfile
import tempfile
import zipfile


def expected_members(binary_name):
    if binary_name in ("", ".", "..", "config.example.json", "README.md") or any(
        character in binary_name for character in "/\\:"
    ):
        raise ValueError("binary name must be a plain file name")
    return {binary_name: 0o755, "config.example.json": 0o644, "README.md": 0o644}


def create_archive(archive_path, binary_name, package_directory):
    expected = expected_members(binary_name)
    archive_path = Path(archive_path)
    package_directory = Path(package_directory)
    for name in expected:
        source = package_directory / name
        if source.is_symlink() or not source.is_file():
            raise ValueError(f"package source must be a regular file: {source}")
    archive_path.parent.mkdir(parents=True, exist_ok=True)
    if archive_path.name.endswith(".tar.gz"):
        with tarfile.open(archive_path, "w:gz", format=tarfile.USTAR_FORMAT) as archive:
            for name, mode in expected.items():
                source = package_directory / name
                info = tarfile.TarInfo(name)
                info.mode = mode
                info.size = source.stat().st_size
                with source.open("rb") as content:
                    archive.addfile(info, content)
    elif archive_path.name.endswith(".zip"):
        with zipfile.ZipFile(archive_path, "w") as archive:
            for name, mode in expected.items():
                info = zipfile.ZipInfo(name)
                info.create_system = 3
                info.external_attr = (stat.S_IFREG | mode) << 16
                info.compress_type = zipfile.ZIP_DEFLATED
                archive.writestr(info, (package_directory / name).read_bytes())
    else:
        raise ValueError("archive must be .tar.gz or .zip")


def verify_archive(archive_path, binary_name):
    expected = expected_members(binary_name)
    archive_path = Path(archive_path)

    def check_names(names):
        if len(names) != len(expected) or set(names) != set(expected):
            raise ValueError(f"archive must contain exactly {sorted(expected)}, got {names}")

    if archive_path.name.endswith(".tar.gz"):
        with tarfile.open(archive_path, "r:gz") as archive:
            members = archive.getmembers()
            check_names([member.name for member in members])
            for member in members:
                if not member.isreg():
                    raise ValueError(f"not a regular file: {member.name}")
                if member.uid != 0 or member.gid != 0 or member.uname or member.gname:
                    raise ValueError(f"non-normalized ownership: {member.name}")
                if member.mode != expected[member.name]:
                    raise ValueError(f"unexpected permissions: {member.name}: {member.mode:o}")
    elif archive_path.name.endswith(".zip"):
        with zipfile.ZipFile(archive_path) as archive:
            members = archive.infolist()
            check_names([member.filename for member in members])
            for member in members:
                mode = member.external_attr >> 16
                if not stat.S_ISREG(mode):
                    raise ValueError(f"not a regular file: {member.filename}")
                if stat.S_IMODE(mode) != expected[member.filename]:
                    raise ValueError(f"unexpected permissions: {member.filename}: {mode:o}")
                if member.extra:
                    raise ValueError(f"unexpected extra metadata: {member.filename}")
            corrupt = archive.testzip()
            if corrupt is not None:
                raise ValueError(f"invalid ZIP checksum: {corrupt}")
    else:
        raise ValueError("archive must be .tar.gz or .zip")


def verify_extraction(archive_path):
    # POSIX metadata is checked on the Linux release runner, never on a deployment directory.
    if os.name != "posix":
        return False
    archive_path = Path(archive_path).resolve()
    with tempfile.TemporaryDirectory(prefix="gosub-release-check-") as temporary:
        destination = Path(temporary) / "extract"
        destination.mkdir(mode=0o700)
        config_path = destination / "config.json"
        database_path = destination / "gosub.db"
        config_path.write_bytes(b"existing configuration\n")
        database_path.write_bytes(b"existing database\n")
        before = destination.stat()
        if archive_path.name.endswith(".tar.gz"):
            subprocess.run(["tar", "-xzf", str(archive_path), "-C", str(destination)], check=True)
        else:
            with zipfile.ZipFile(archive_path) as archive:
                archive.extractall(destination)
        after = destination.stat()
        if (stat.S_IMODE(before.st_mode), before.st_uid, before.st_gid) != (
            stat.S_IMODE(after.st_mode), after.st_uid, after.st_gid
        ):
            raise ValueError("extraction changed destination directory permissions or ownership")
        if config_path.read_bytes() != b"existing configuration\n":
            raise ValueError("extraction overwrote existing config.json")
        if database_path.read_bytes() != b"existing database\n":
            raise ValueError("extraction overwrote existing gosub.db")
    return True


def main():
    parser = argparse.ArgumentParser(description="Create and validate safe GoSub release archives.")
    parser.add_argument("archive", type=Path)
    parser.add_argument("binary_name")
    parser.add_argument("--package-dir", type=Path, help="create the archive from these package files first")
    args = parser.parse_args()
    if args.package_dir is not None:
        create_archive(args.archive, args.binary_name, args.package_dir)
    verify_archive(args.archive, args.binary_name)
    print(f"Archive contents and permissions verified: {args.archive}")
    if verify_extraction(args.archive):
        print("Extraction preserved destination directory metadata and existing runtime data.")
    else:
        print("POSIX extraction metadata check requires Linux; skipped on this platform.")


if __name__ == "__main__":
    main()
