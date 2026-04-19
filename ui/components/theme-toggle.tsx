"use client";

import { MoonStar, SunMedium } from "lucide-react";
import { secondaryButtonClassName } from "./ui";
import { useTheme } from "./theme-provider";

export function ThemeToggle() {
  const { theme, toggleTheme } = useTheme();
  const nextModeLabel = theme === "dark" ? "Light mode" : "Dark mode";

  return (
    <button
      type="button"
      onClick={toggleTheme}
      className={`${secondaryButtonClassName()} inline-flex items-center gap-2 whitespace-nowrap`}
      aria-label={`Switch to ${nextModeLabel.toLowerCase()}`}
    >
      {theme === "dark" ? <SunMedium className="h-4 w-4" /> : <MoonStar className="h-4 w-4" />}
      <span>{nextModeLabel}</span>
    </button>
  );
}