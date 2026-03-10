import type { LocalTool } from "@/api/types";

interface Props {
  tools: LocalTool[];
  selected: Set<string>;
  onToggle: (name: string) => void;
}

export function LocalToolsPicker({ tools, selected, onToggle }: Props) {
  if (tools.length === 0) {
    return <p className="muted">No local tools published by this manager.</p>;
  }

  return (
    <div className="stack-sm">
      {tools.map((tool) => (
        <label key={tool.name} className="tool-card row" style={{ margin: 0, gap: 10 }}>
          <input
            type="checkbox"
            checked={selected.has(tool.name)}
            onChange={() => onToggle(tool.name)}
          />
          <div style={{ flex: 1 }}>
            <div style={{ fontWeight: 600 }}>{tool.name}</div>
            {tool.description && (
              <div className="muted" style={{ fontSize: 13 }}>
                {tool.description}
              </div>
            )}
            {tool.requirements && tool.requirements.length > 0 && (
              <div className="muted" style={{ fontSize: 12 }}>
                Requires: {tool.requirements.join(", ")}
              </div>
            )}
            {tool.commands && tool.commands.length > 0 && (
              <div className="muted" style={{ fontSize: 12 }}>
                Commands:{" "}
                {tool.commands.map((c) => (
                  <code key={c.name}>{c.name}</code>
                ))}
              </div>
            )}
          </div>
        </label>
      ))}
    </div>
  );
}
