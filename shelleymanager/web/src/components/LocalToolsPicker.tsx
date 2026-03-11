import type { LocalTool } from "@/api/types";

interface Props {
  tools: LocalTool[];
  selected: Set<string>;
  onToggle: (name: string) => void;
}

export function LocalToolsPicker({ tools, selected, onToggle }: Props) {
  const visibleTools = tools.filter((tool) => tool.exposure !== "support_bundle");

  if (visibleTools.length === 0) {
    return <p className="muted">No local tools published by this manager.</p>;
  }

  return (
    <div className="stack-sm">
      {visibleTools.map((tool) => (
        <div key={tool.name} className="tool-card tool-card-choice">
          <div className="tool-card-head">
            <input
              id={`local-tool-${tool.name}`}
              className="tool-card-input"
              type="checkbox"
              checked={selected.has(tool.name)}
              onChange={() => onToggle(tool.name)}
            />
            <label htmlFor={`local-tool-${tool.name}`} className="tool-card-title">
              {tool.name}
            </label>
          </div>
          <div className="tool-card-copy">
            {tool.description && (
              <div className="tool-card-description">{tool.description}</div>
            )}
            {tool.requirements && tool.requirements.length > 0 && (
              <div className="tool-card-meta">
                Requires: {tool.requirements.join(", ")}
              </div>
            )}
            {tool.commands && tool.commands.length > 0 && (
              <div className="tool-card-meta">
                Command:{" "}
                {tool.commands.map((c) => (
                  <code key={c.name}>{c.name}</code>
                ))}
              </div>
            )}
          </div>
        </div>
      ))}
    </div>
  );
}
