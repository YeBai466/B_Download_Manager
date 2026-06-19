import { api } from "../api";
import { t } from "../i18n";

export interface UpdateInfo {
  current: string;
  latest: string;
  hasUpdate: boolean;
  notes: string;
  releaseUrl: string;
  downloadUrl: string;
  publishedAt: string;
}

interface Props {
  info: UpdateInfo;
  onClose: () => void;
}

// UpdateDialog shows the new version and its release notes (changelog). It does
// not auto-update — the user downloads the installer; reinstalling preserves all
// data/settings (they live in %AppData%, not the install dir).
export default function UpdateDialog({ info, onClose }: Props) {
  const download = () => {
    api.openURL(info.downloadUrl || info.releaseUrl);
  };

  return (
    <div className="overlay" onMouseDown={onClose}>
      <div className="dialog" style={{ width: 560 }} onMouseDown={(e) => e.stopPropagation()}>
        <div className="titlebar">{t("upd.title")}</div>
        <div className="content">
          <div className="info-grid" style={{ marginBottom: 14 }}>
            <span className="k">{t("upd.current")}</span><span className="v">{info.current}</span>
            <span className="k">{t("upd.latest")}</span><span className="v" style={{ color: "#2a9d4a", fontWeight: 600 }}>{info.latest}</span>
          </div>
          <div style={{ fontSize: 12, color: "#44505f", fontWeight: 500, marginBottom: 6 }}>{t("upd.changelog")}</div>
          <div className="changelog">{info.notes || t("upd.noNotes")}</div>
          <div className="note" style={{ marginTop: 12 }}>
            {t("upd.note")}
          </div>
        </div>
        <div className="actions">
          <button className="btn" onClick={onClose}>{t("upd.later")}</button>
          {info.releaseUrl && <button className="btn" onClick={() => api.openURL(info.releaseUrl)}>{t("upd.viewRelease")}</button>}
          <button className="btn primary" onClick={download}>{t("upd.download")}</button>
        </div>
      </div>
    </div>
  );
}
