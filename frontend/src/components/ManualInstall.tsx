import { useEffect, useState } from "react";
import { api, type ExtStatus } from "../api";

const BROWSERS = ["Chrome", "Edge"] as const;
type Browser = (typeof BROWSERS)[number];

const extUrl = (b: Browser) => (b === "Edge" ? "edge://extensions" : "chrome://extensions");
const storeUrl = (id: string) => `https://chromewebstore.google.com/detail/${id}`;

// ManualInstall manages browser-extension installation. The extension is now
// published on the Chrome Web Store, so the primary path is a one-click policy
// force-install (works on consumer machines because the source is the store).
// A manual "Load unpacked" path remains as a fallback.
export default function ManualInstall() {
  const [installed, setInstalled] = useState<string[]>([]);
  const [sel, setSel] = useState<Record<string, boolean>>({});
  const [status, setStatus] = useState<ExtStatus | null>(null);
  const [dir, setDir] = useState("");
  const [busy, setBusy] = useState("");
  const [msg, setMsg] = useState("");
  const [error, setError] = useState("");
  const [showManual, setShowManual] = useState(false);

  const refresh = () => api.extStatus().then(setStatus).catch(() => {});

  useEffect(() => {
    api.installedBrowsers().then((b) => {
      const list = b ?? [];
      setInstalled(list);
      const init: Record<string, boolean> = {};
      for (const x of BROWSERS) if (list.includes(x)) init[x] = true; // pre-check all installed
      setSel(init);
    });
    refresh();
  }, []);

  const chosen = () => BROWSERS.filter((b) => sel[b] && installed.includes(b));

  const install = async () => {
    const c = chosen();
    if (c.length === 0) { setError("请至少选择一个已安装的浏览器"); return; }
    setBusy("install"); setError(""); setMsg("");
    try {
      await api.installExt(c);
      setMsg("已写入安装策略。请重启 " + c.join(" / ") + " 后生效（地址栏输入 chrome://restart）。");
      setTimeout(refresh, 1500);
    } catch (e: any) {
      setError(String(e?.message ?? e));
    } finally { setBusy(""); }
  };

  const uninstall = async () => {
    const c = chosen();
    setBusy("uninstall"); setError(""); setMsg("");
    try {
      await api.uninstallExt(c.length ? c : [...BROWSERS]);
      setMsg("已移除安装策略。重启浏览器后扩展将被卸载。");
      setTimeout(refresh, 1500);
    } catch (e: any) {
      setError(String(e?.message ?? e));
    } finally { setBusy(""); }
  };

  const manual = async () => {
    setBusy("manual"); setError("");
    try {
      const info = await api.prepareManualInstall();
      setDir(info.dir);
    } catch (e: any) {
      setError(String(e?.message ?? e));
    } finally { setBusy(""); }
  };

  const id = status?.id ?? "";

  return (
    <div>
      <div className="field">
        <label>选择浏览器</label>
        <div className="row" style={{ gap: 18 }}>
          {BROWSERS.map((b) => {
            const has = installed.includes(b);
            const on = b === "Chrome" ? status?.chrome : status?.edge;
            return (
              <label key={b} className="checkbox" style={{ opacity: has ? 1 : 0.45 }}>
                <input type="checkbox" disabled={!has} checked={!!sel[b]}
                  onChange={(e) => setSel((s) => ({ ...s, [b]: e.target.checked }))} />
                {b}{!has && "（未安装）"}
                {has && <span className={`status-pill ${on ? "ok" : "no"}`} style={{ marginLeft: 6 }}><span className="dot" />{on ? "已配置" : "未配置"}</span>}
              </label>
            );
          })}
        </div>
      </div>

      <div className="field">
        <div className="row">
          <button className="btn primary" onClick={install} disabled={busy !== ""}>
            {busy === "install" ? "安装中…" : "一键安装（需管理员授权）"}
          </button>
          <button className="btn danger" onClick={uninstall} disabled={busy !== ""}>
            {busy === "uninstall" ? "卸载中…" : "卸载"}
          </button>
          <button className="btn" onClick={refresh} disabled={busy !== ""}>刷新状态</button>
        </div>
        <span className="hint">
          扩展已发布在 Chrome 网上应用店，本机通过企业策略静默安装（会弹出一次 UAC 管理员授权）。
          安装后请<strong>重启浏览器</strong>生效；浏览器会显示「由你的组织安装」，可随时点「卸载」移除。
        </span>
      </div>

      {msg && <div className="status-text" style={{ color: "#2a9d4a" }}>{msg}</div>}
      {error && <div className="status-text err">{error}</div>}

      <div className="field" style={{ marginTop: 8 }}>
        <div className="row" style={{ gap: 14 }}>
          {id && <a href={storeUrl(id)} target="_blank" rel="noreferrer">在网上应用店查看</a>}
          <a onClick={(e) => { e.preventDefault(); setShowManual((v) => !v); }} href="#" >
            {showManual ? "收起手动安装" : "改用手动安装（开发者模式）"}
          </a>
        </div>
      </div>

      {showManual && (
        <div className="note">
          <p style={{ margin: "0 0 6px" }}>若一键安装不可用，可手动加载（一次永久生效）：</p>
          <button className="btn" onClick={manual} disabled={busy !== ""}>
            {busy === "manual" ? "准备中…" : "准备扩展文件夹"}
          </button>
          {dir && (
            <ol style={{ margin: "10px 0 0", paddingLeft: 18, lineHeight: 2 }}>
              <li>在浏览器地址栏打开 <code>{extUrl(chosen()[0] ?? "Chrome")}</code></li>
              <li>打开右上角「开发者模式」</li>
              <li>点「加载已解压的扩展程序」，选择：<br /><code>{dir}</code></li>
            </ol>
          )}
        </div>
      )}
    </div>
  );
}
