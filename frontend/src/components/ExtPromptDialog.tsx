import ManualInstall from "./ManualInstall";
import { t } from "../i18n";

interface Props {
  onLater: () => void;
  onIgnore: () => void;
}

// Shown on startup when browser takeover is enabled (unless the user chose to
// ignore). The extension is published on the Chrome Web Store, so it offers a
// one-click policy install (with a manual fallback). Nothing is installed
// without the user clicking install.
export default function ExtPromptDialog({ onLater, onIgnore }: Props) {
  return (
    <div className="overlay" onMouseDown={(e) => e.stopPropagation()}>
      <div className="dialog" style={{ width: 520 }}>
        <div className="titlebar">{t("ext.title")}</div>
        <div className="content">
          <p style={{ margin: "0 0 12px", lineHeight: 1.7 }}>
            {t("ext.intro")}
          </p>
          <ManualInstall />
        </div>
        <div className="actions">
          <button className="btn" onClick={onIgnore}>{t("ext.ignoreForever")}</button>
          <button className="btn primary" onClick={onLater}>{t("common.close")}</button>
        </div>
      </div>
    </div>
  );
}
