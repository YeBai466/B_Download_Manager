import { useEffect, useState } from "react";
import { api, type ExtStatus } from "../api";
import { t } from "../i18n";

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
    if (c.length === 0) { setError(t("mi.pickAtLeastOne")); return; }
    setBusy("install"); setError(""); setMsg("");
    try {
      await api.installExt(c);
      setMsg(t("mi.installedPolicy", { browsers: c.join(" / ") }));
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
      setMsg(t("mi.removedPolicy"));
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
        <label>{t("mi.pickBrowser")}</label>
        <div className="row" style={{ gap: 18 }}>
          {BROWSERS.map((b) => {
            const has = installed.includes(b);
            const on = b === "Chrome" ? status?.chrome : status?.edge;
            return (
              <label key={b} className="checkbox" style={{ opacity: has ? 1 : 0.45 }}>
                <input type="checkbox" disabled={!has} checked={!!sel[b]}
                  onChange={(e) => setSel((s) => ({ ...s, [b]: e.target.checked }))} />
                {b}{!has && t("mi.notInstalled")}
                {has && <span className={`status-pill ${on ? "ok" : "no"}`} style={{ marginLeft: 6 }}><span className="dot" />{on ? t("mi.configured") : t("mi.notConfigured")}</span>}
              </label>
            );
          })}
        </div>
      </div>

      <div className="field">
        <div className="row">
          <button className="btn primary" onClick={install} disabled={busy !== ""}>
            {busy === "install" ? t("mi.installing") : t("mi.install")}
          </button>
          <button className="btn danger" onClick={uninstall} disabled={busy !== ""}>
            {busy === "uninstall" ? t("mi.uninstalling") : t("mi.uninstall")}
          </button>
          <button className="btn" onClick={refresh} disabled={busy !== ""}>{t("mi.refresh")}</button>
        </div>
        <span className="hint">
          {t("mi.installHint")}
        </span>
      </div>

      {msg && <div className="status-text" style={{ color: "#2a9d4a" }}>{msg}</div>}
      {error && <div className="status-text err">{error}</div>}

      <div className="field" style={{ marginTop: 8 }}>
        <div className="row" style={{ gap: 14 }}>
          {id && <a href={storeUrl(id)} target="_blank" rel="noreferrer">{t("mi.viewInStore")}</a>}
          <a onClick={(e) => { e.preventDefault(); setShowManual((v) => !v); }} href="#" >
            {showManual ? t("mi.collapseManual") : t("mi.useManual")}
          </a>
        </div>
      </div>

      {showManual && (
        <div className="note">
          <p style={{ margin: "0 0 6px" }}>{t("mi.manualIntro")}</p>
          <button className="btn" onClick={manual} disabled={busy !== ""}>
            {busy === "manual" ? t("mi.preparing") : t("mi.prepareFolder")}
          </button>
          {dir && (
            <ol style={{ margin: "10px 0 0", paddingLeft: 18, lineHeight: 2 }}>
              <li>{t("mi.step1")} <code>{extUrl(chosen()[0] ?? "Chrome")}</code></li>
              <li>{t("mi.step2")}</li>
              <li>{t("mi.step3")}<br /><code>{dir}</code></li>
            </ol>
          )}
        </div>
      )}
    </div>
  );
}
