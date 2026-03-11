import { AboutLink } from "@/components/AboutLink";
import { SettingsButton } from "@/components/SettingsButton";

export function PageMenu({ showAbout = true }: { showAbout?: boolean }) {
  return (
    <div className="row" style={{ gap: 6 }}>
      {showAbout && <AboutLink />}
      <SettingsButton />
    </div>
  );
}
