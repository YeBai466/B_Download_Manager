import ManualInstall from "./ManualInstall";

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
        <div className="titlebar">启用浏览器接管？</div>
        <div className="content">
          <p style={{ margin: "0 0 12px", lineHeight: 1.7 }}>
            安装浏览器扩展后，Chrome / Edge 里的下载会自动交给本程序多线程下载。
            勾选浏览器后点「一键安装」即可（需一次管理员授权，装后重启浏览器生效）。
          </p>
          <ManualInstall />
        </div>
        <div className="actions">
          <button className="btn" onClick={onIgnore}>从此忽略</button>
          <button className="btn primary" onClick={onLater}>关闭</button>
        </div>
      </div>
    </div>
  );
}
