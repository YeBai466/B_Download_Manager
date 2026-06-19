import type { TaskInfo } from "../api";
import * as Ico from "../icons";
import { t } from "../i18n";

export type Filter =
  | { kind: "all" }
  | { kind: "active" }
  | { kind: "done" }
  | { kind: "category"; name: string };

interface Props {
  categories: string[];
  tasks: TaskInfo[];
  filter: Filter;
  onSelect: (f: Filter) => void;
}

function same(a: Filter, b: Filter): boolean {
  if (a.kind !== b.kind) return false;
  if (a.kind === "category" && b.kind === "category") return a.name === b.name;
  return true;
}

export default function Sidebar({ categories, tasks, filter, onSelect }: Props) {
  const counts = {
    all: tasks.length,
    active: tasks.filter((t) => t.status !== "completed").length,
    done: tasks.filter((t) => t.status === "completed").length,
  };
  const catCount = (name: string) => tasks.filter((t) => t.category === name).length;

  const Item = ({
    f,
    icon,
    label,
    count,
  }: {
    f: Filter;
    icon: React.ReactNode;
    label: string;
    count: number;
  }) => (
    <div className={`side-item${same(filter, f) ? " active" : ""}`} onClick={() => onSelect(f)}>
      <span className="ico">{icon}</span>
      <span>{label}</span>
      <span className="count">{count}</span>
    </div>
  );

  return (
    <div className="sidebar">
      <Item f={{ kind: "all" }} icon={<Ico.CatAll />} label={t("side.all")} count={counts.all} />
      <Item f={{ kind: "active" }} icon={<Ico.CatUnfinished />} label={t("side.active")} count={counts.active} />
      <Item f={{ kind: "done" }} icon={<Ico.CatFinished />} label={t("side.done")} count={counts.done} />
      <div className="side-section">{t("side.categories")}</div>
      {categories.map((c) => {
        const Icon = Ico.categoryIcon[c] ?? Ico.CatDocuments;
        return (
          <Item
            key={c}
            f={{ kind: "category", name: c }}
            icon={<Icon />}
            label={categoryLabel(c)}
            count={catCount(c)}
          />
        );
      })}
    </div>
  );
}

function categoryLabel(c: string): string {
  const key = `cat.${c}`;
  const label = t(key);
  return label === key ? c : label;
}
export { categoryLabel };
