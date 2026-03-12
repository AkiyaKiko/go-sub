import { useCallback, useEffect, useMemo, useState } from "react";
import type { FormEvent } from "react";

type TLSConfig = {
  enabled: boolean;
  certFile: string;
  keyFile: string;
};

type Settings = {
  host: string;
  port: number;
  path: string;
  admin: string;
  password: string;
  tls?: TLSConfig;
};

type ConfigResponse = {
  settings: Settings;
  nodes: string[];
  subscription_url: string;
};

function generateRandomPath(length = 8): string {
  const alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789";
  const result: string[] = ["/"];

  if (typeof crypto !== "undefined" && typeof crypto.getRandomValues === "function") {
    const bytes = new Uint8Array(length);
    crypto.getRandomValues(bytes);
    for (let i = 0; i < bytes.length; i += 1) {
      result.push(alphabet[bytes[i] % alphabet.length]);
    }
    return result.join("");
  }

  for (let i = 0; i < length; i += 1) {
    result.push(alphabet[Math.floor(Math.random() * alphabet.length)]);
  }
  return result.join("");
}

async function apiFetch(path: string, init?: RequestInit): Promise<Response> {
  return fetch(path, {
    ...init,
    credentials: "include",
  });
}

async function getErrorText(resp: Response): Promise<string> {
  const text = await resp.text();
  return text || `HTTP ${resp.status}`;
}

export default function App() {
  const [isCheckingAuth, setIsCheckingAuth] = useState(true);
  const [isAuthenticated, setIsAuthenticated] = useState(false);
  const [loginUser, setLoginUser] = useState("");
  const [loginPassword, setLoginPassword] = useState("");
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [message, setMessage] = useState("");

  const [config, setConfig] = useState<ConfigResponse | null>(null);
  const [pathInput, setPathInput] = useState("");
  const [nodesText, setNodesText] = useState("");

  const parsedNodes = useMemo(() => {
    return nodesText
      .split(/\r?\n/)
      .map((v) => v.trim())
      .filter((v) => v.length > 0);
  }, [nodesText]);

  const loadConfig = useCallback(async () => {
    const resp = await apiFetch("/api/config", { method: "GET" });
    if (!resp.ok) {
      throw new Error(await getErrorText(resp));
    }
    const data = (await resp.json()) as ConfigResponse;
    setConfig(data);
    setPathInput(data.settings.path);
    setNodesText(data.nodes.join("\n"));
  }, []);

  const checkSession = useCallback(async () => {
    setIsCheckingAuth(true);
    try {
      const resp = await apiFetch("/api/session", { method: "GET" });
      if (!resp.ok) {
        setIsAuthenticated(false);
        setConfig(null);
        return;
      }
      setIsAuthenticated(true);
      await loadConfig();
    } catch {
      setIsAuthenticated(false);
      setConfig(null);
    } finally {
      setIsCheckingAuth(false);
    }
  }, [loadConfig]);

  useEffect(() => {
    void checkSession();
  }, [checkSession]);

  async function onLogin(e: FormEvent<HTMLFormElement>) {
    e.preventDefault();
    setIsSubmitting(true);
    setMessage("");

    try {
      const body = new URLSearchParams();
      body.set("username", loginUser);
      body.set("password", loginPassword);

      const resp = await apiFetch("/api/login", {
        method: "POST",
        body,
      });

      if (!resp.ok) {
        setMessage(`登录失败：${await getErrorText(resp)}`);
        return;
      }

      setIsAuthenticated(true);
      setLoginPassword("");
      await loadConfig();
      setMessage("登录成功");
    } catch {
      setMessage("登录失败：网络或服务异常");
    } finally {
      setIsSubmitting(false);
    }
  }

  async function onLogout() {
    setIsSubmitting(true);
    setMessage("");
    try {
      await apiFetch("/api/logout", { method: "POST" });
    } finally {
      setIsSubmitting(false);
      setIsAuthenticated(false);
      setConfig(null);
      setLoginPassword("");
      setMessage("已退出登录");
    }
  }

  async function onSavePath() {
    if (!pathInput.startsWith("/")) {
      setMessage("订阅路径必须以 / 开头");
      return;
    }
    setIsSubmitting(true);
    setMessage("");
    try {
      const body = new URLSearchParams();
      body.set("uri", pathInput);
      const resp = await apiFetch("/api/change_uri", {
        method: "POST",
        body,
      });
      if (!resp.ok) {
        setMessage(`保存路径失败：${await getErrorText(resp)}`);
        return;
      }
      await loadConfig();
      setMessage("订阅路径已保存");
    } catch {
      setMessage("保存路径失败：网络或服务异常");
    } finally {
      setIsSubmitting(false);
    }
  }

  function onGeneratePath() {
    setPathInput(generateRandomPath());
    setMessage("已生成随机路径，请点击“保存路径”生效");
  }

  async function onSaveNodes() {
    setIsSubmitting(true);
    setMessage("");
    try {
      const body = new URLSearchParams();
      parsedNodes.forEach((node) => body.append("nodes", node));
      const resp = await apiFetch("/api/change_nodes", {
        method: "POST",
        body,
      });
      if (!resp.ok) {
        setMessage(`保存节点失败：${await getErrorText(resp)}`);
        return;
      }
      await loadConfig();
      setMessage("节点列表已保存");
    } catch {
      setMessage("保存节点失败：网络或服务异常");
    } finally {
      setIsSubmitting(false);
    }
  }

  if (isCheckingAuth) {
    return <main className="p-6">正在检查登录状态...</main>;
  }

  if (!isAuthenticated) {
    return (
      <main className="mx-auto max-w-md p-6">
        <h1 className="mb-4 text-2xl font-bold">管理后台登录</h1>
        <form className="flex flex-col gap-3" onSubmit={onLogin}>
          <input
            className="rounded border px-3 py-2"
            value={loginUser}
            placeholder="用户名"
            onChange={(e) => setLoginUser(e.target.value)}
          />
          <input
            className="rounded border px-3 py-2"
            value={loginPassword}
            placeholder="密码"
            type="password"
            onChange={(e) => setLoginPassword(e.target.value)}
          />
          <button
            className="rounded bg-black px-3 py-2 text-white disabled:opacity-50"
            disabled={isSubmitting}
            type="submit"
          >
            {isSubmitting ? "登录中..." : "登录"}
          </button>
        </form>
        {message && <p className="mt-4 text-sm">{message}</p>}
      </main>
    );
  }

  return (
    <main className="mx-auto flex max-w-4xl flex-col gap-6 p-6">
      <header className="flex items-center justify-between">
        <h1 className="text-2xl font-bold">代理订阅管理</h1>
        <button
          className="rounded border px-3 py-2 disabled:opacity-50"
          onClick={() => void onLogout()}
          disabled={isSubmitting}
          type="button"
        >
          退出登录
        </button>
      </header>

      {config && (
        <section className="rounded border p-4">
          <h2 className="mb-3 text-lg font-semibold">当前服务信息</h2>
          <div className="space-y-1 text-sm">
            <p>地址: {config.settings.host}:{config.settings.port}</p>
            <p>TLS: {config.settings.tls?.enabled ? "开启" : "关闭"}</p>
            <p>订阅链接: {config.subscription_url}</p>
            <p>管理员: {config.settings.admin}</p>
          </div>
        </section>
      )}

      <section className="rounded border p-4">
        <h2 className="mb-3 text-lg font-semibold">订阅路径</h2>
        <div className="flex gap-2">
          <input
            className="w-full rounded border px-3 py-2"
            value={pathInput}
            onChange={(e) => setPathInput(e.target.value)}
          />
          <button
            className="rounded border px-3 py-2 disabled:opacity-50"
            disabled={isSubmitting}
            type="button"
            onClick={onGeneratePath}
          >
            随机生成
          </button>
          <button
            className="rounded bg-black px-3 py-2 text-white disabled:opacity-50"
            disabled={isSubmitting}
            type="button"
            onClick={() => void onSavePath()}
          >
            保存路径
          </button>
        </div>
      </section>

      <section className="rounded border p-4">
        <h2 className="mb-3 text-lg font-semibold">全部订阅连接（每行一个）</h2>
        <textarea
          className="min-h-64 w-full rounded border p-3 font-mono text-sm"
          value={nodesText}
          onChange={(e) => setNodesText(e.target.value)}
        />
        <div className="mt-3 flex items-center justify-between">
          <p className="text-sm text-gray-600">当前共 {parsedNodes.length} 条连接</p>
          <button
            className="rounded bg-black px-3 py-2 text-white disabled:opacity-50"
            disabled={isSubmitting}
            type="button"
            onClick={() => void onSaveNodes()}
          >
            保存连接
          </button>
        </div>
      </section>

      {message && <p className="text-sm">{message}</p>}
    </main>
  );
}
