import { useEffect, useMemo, useState } from "react";
import type { TaskInfo } from "../api";
import { formatBytes, formatSpeed, formatETA, formatDate, statusLabel } from "../format";
import * as Ico from "../icons";
import { t as tr } from "../i18n";

interface Props {
  tasks: TaskInfo[];
  selectedId: string | null;
  onSelect: (id: string) => void;
  onResume: (id: string) => void;
  onPause: (id: string) => void;
  onRemove: (id: string, deleteFile: boolean) => void;
  onOpenFile: (id: string) => void;
  onOpenFolder: (id: string) => void;
  onDetails: (id: string) => void;
  onCopyUrl: (url: string) => void;
}

type SortKey = "name" | "size" | "status" | "created";
interface SortState { key: SortKey; dir: 1 | -1; }
interface CtxMenu { x: number; y: number; task: TaskInfo; }

function ProgressCell({ t }: { t: TaskInfo }) {
  if (t.status === "completed") return <span className="status-text done">{statusLabel("completed")}</span>;
  if (t.status === "error") return <span className="status-text err" title={t.error}>{t.error || tr("tbl.error")}</span>;
  const pct = t.progress >= 0 ? Math.round(t.progress * 100) : -1;
  const cls = t.status === "paused" ? "bar paused" : "bar";
  return (
    <div className={cls}>
      <div className="fill" style={{ width: pct >= 0 ? `${pct}%` : "0%" }} />
      <div className="label">{pct >= 0 ? `${pct}%` : statusLabel(t.status)}</div>
    </div>
  );
}

export default function TaskTable(p: Props) {
  const [sort, setSort] = useState<SortState>({ key: "created", dir: -1 });
  const [menu, setMenu] = useState<CtxMenu | null>(null);

  useEffect(() => {
    if (!menu) return;
    const close = () => setMenu(null);
    window.addEventListener("click", close);
    window.addEventListener("resize", close);
    window.addEventListener("scroll", close, true);
    return () => {
      window.removeEventListener("click", close);
      window.removeEventListener("resize", close);
      window.removeEventListener("scroll", close, true);
    };
  }, [menu]);

  const sorted = useMemo(() => {
    const arr = [...p.tasks];
    const d = sort.dir;
    arr.sort((a, b) => {
      switch (sort.key) {
        case "name": return d * a.filename.localeCompare(b.filename);
        case "size": return d * (a.totalSize - b.totalSize);
        case "status": return d * a.status.localeCompare(b.status);
        default: return d * (a.createdAt < b.createdAt ? -1 : 1);
      }
    });
    return arr;
  }, [p.tasks, sort]);

  const toggleSort = (key: SortKey) =>
    setSort((s) => (s.key === key ? { key, dir: s.dir === 1 ? -1 : 1 } : { key, dir: 1 }));

  const arrow = (key: SortKey) => (sort.key === key ? <span className="sort">{sort.dir === 1 ? "▲" : "▼"}</span> : null);

  if (p.tasks.length === 0) {
    return (
      <div className="table-wrap">
        <div className="empty">
          <div className="big"><Ico.AddUrl size={48} /></div>
          <div>{tr("tbl.empty")}</div>
          <div>{tr("tbl.emptyHint")}</div>
        </div>
      </div>
    );
  }

  const openContext = (e: React.MouseEvent, t: TaskInfo) => {
    e.preventDefault();
    p.onSelect(t.id);
    setMenu({ x: e.clientX, y: e.clientY, task: t });
  };

  const onDouble = (t: TaskInfo) => {
    if (t.status === "completed") p.onOpenFile(t.id);
    else p.onDetails(t.id);
  };

  return (
    <div className="table-wrap">
      <table className="tasks">
        <colgroup>
          <col style={{ width: "34%" }} />
          <col style={{ width: 88 }} />
          <col style={{ width: 170 }} />
          <col style={{ width: 92 }} />
          <col style={{ width: 96 }} />
          <col style={{ width: 132 }} />
        </colgroup>
        <thead>
          <tr>
            <th onClick={() => toggleSort("name")}>{tr("tbl.name")}{arrow("name")}</th>
            <th onClick={() => toggleSort("size")}>{tr("tbl.size")}{arrow("size")}</th>
            <th onClick={() => toggleSort("status")}>{tr("tbl.status")}{arrow("status")}</th>
            <th>{tr("tbl.eta")}</th>
            <th>{tr("tbl.speed")}</th>
            <th onClick={() => toggleSort("created")}>{tr("tbl.added")}{arrow("created")}</th>
          </tr>
        </thead>
        <tbody>
          {sorted.map((t) => {
            const Icon = Ico.categoryIcon[t.category] ?? Ico.CatDocuments;
            return (
              <tr
                key={t.id}
                className={t.id === p.selectedId ? "selected" : ""}
                onClick={() => p.onSelect(t.id)}
                onContextMenu={(e) => openContext(e, t)}
                onDoubleClick={() => onDouble(t)}
              >
                <td title={t.filename}>
                  <span className="fname">
                    <span className="ico"><Icon /></span>
                    <span className="nm">{t.filename}</span>
                  </span>
                </td>
                <td>{formatBytes(t.totalSize)}</td>
                <td className="cell-progress"><ProgressCell t={t} /></td>
                <td>{t.status === "downloading" ? formatETA(t.etaSeconds) : ""}</td>
                <td>{t.status === "downloading" ? formatSpeed(t.speed) : ""}</td>
                <td>{formatDate(t.createdAt)}</td>
              </tr>
            );
          })}
        </tbody>
      </table>

      {menu && <ContextMenu menu={menu} p={p} close={() => setMenu(null)} />}
    </div>
  );
}

function ContextMenu({ menu, p, close }: { menu: CtxMenu; p: Props; close: () => void }) {
  const t = menu.task;
  const active = t.status === "downloading" || t.status === "connecting" || t.status === "queued";
  const done = t.status === "completed";
  const run = (fn: () => void) => () => { fn(); close(); };

  // Clamp the menu so it stays on screen.
  const style: React.CSSProperties = {
    left: Math.min(menu.x, window.innerWidth - 210),
    top: Math.min(menu.y, window.innerHeight - 320),
  };

  return (
    <div className="ctx" style={style} onClick={(e) => e.stopPropagation()}>
      {done && <div className="ctx-item" onClick={run(() => p.onOpenFile(t.id))}>{tr("ctx.open")}</div>}
      <div className="ctx-item" onClick={run(() => p.onOpenFolder(t.id))}>{tr("ctx.openFolder")}</div>
      <div className="ctx-sep" />
      {!active && !done && <div className="ctx-item" onClick={run(() => p.onResume(t.id))}>{tr("ctx.resume")}</div>}
      {active && <div className="ctx-item" onClick={run(() => p.onPause(t.id))}>{tr("ctx.pause")}</div>}
      {!done && <div className="ctx-item" onClick={run(() => p.onDetails(t.id))}>{tr("ctx.details")}</div>}
      <div className="ctx-item" onClick={run(() => p.onCopyUrl(t.url))}>{tr("ctx.copyUrl")}</div>
      <div className="ctx-sep" />
      <div className="ctx-item" onClick={run(() => p.onRemove(t.id, false))}>{tr("ctx.removeFromList")}</div>
      <div className="ctx-item danger" onClick={run(() => p.onRemove(t.id, true))}>{tr("ctx.deleteWithFile")}</div>
    </div>
  );
}
