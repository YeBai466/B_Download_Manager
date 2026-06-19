import * as Ico from "../icons";
import { t } from "../i18n";

interface Props {
  canResume: boolean;
  canStop: boolean;
  hasSelection: boolean;
  hasCompleted: boolean;
  onAdd: () => void;
  onResume: () => void;
  onStop: () => void;
  onStartAll: () => void;
  onStopAll: () => void;
  onDelete: () => void;
  onDeleteCompleted: () => void;
  onOpenFolder: () => void;
  onOptions: () => void;
}

function Btn({
  icon,
  label,
  onClick,
  disabled,
}: {
  icon: React.ReactNode;
  label: string;
  onClick: () => void;
  disabled?: boolean;
}) {
  return (
    <button className="tb-btn" onClick={onClick} disabled={disabled} title={label}>
      {icon}
      <span>{label}</span>
    </button>
  );
}

export default function Toolbar(p: Props) {
  return (
    <div className="toolbar">
      <Btn icon={<Ico.AddUrl />} label={t("tb.addUrl")} onClick={p.onAdd} />
      <div className="tb-sep" />
      <Btn icon={<Ico.Resume />} label={t("tb.start")} onClick={p.onResume} disabled={!p.canResume} />
      <Btn icon={<Ico.Stop />} label={t("tb.pause")} onClick={p.onStop} disabled={!p.canStop} />
      <Btn icon={<Ico.StartAll />} label={t("tb.startAll")} onClick={p.onStartAll} />
      <Btn icon={<Ico.StopAll />} label={t("tb.pauseAll")} onClick={p.onStopAll} />
      <div className="tb-sep" />
      <Btn icon={<Ico.Delete />} label={t("tb.delete")} onClick={p.onDelete} disabled={!p.hasSelection} />
      <Btn icon={<Ico.DeleteDone />} label={t("tb.deleteCompleted")} onClick={p.onDeleteCompleted} disabled={!p.hasCompleted} />
      <div className="tb-sep" />
      <Btn icon={<Ico.FolderOpen />} label={t("tb.openFolder")} onClick={p.onOpenFolder} disabled={!p.hasSelection} />
      <div className="tb-spacer" />
      <Btn icon={<Ico.Options />} label={t("tb.options")} onClick={p.onOptions} />
    </div>
  );
}
