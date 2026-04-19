/** @type {import('tailwindcss').Config} */
module.exports = {
  darkMode: ["class"],
  content: [
    "./app/**/*.{js,ts,jsx,tsx,mdx}",
    "./components/**/*.{js,ts,jsx,tsx,mdx}",
    "./lib/**/*.{js,ts,jsx,tsx,mdx}",
  ],
  theme: {
    extend: {
      boxShadow: {
        aura: "var(--shadow-aura)",
      },
      colors: {
        canvas: "rgb(var(--color-canvas) / <alpha-value>)",
        ink: "rgb(var(--color-ink) / <alpha-value>)",
        lagoon: "rgb(var(--color-lagoon) / <alpha-value>)",
        "lagoon-hover": "rgb(var(--color-lagoon-hover) / <alpha-value>)",
        ember: "rgb(var(--color-ember) / <alpha-value>)",
        mist: "rgb(var(--color-mist) / <alpha-value>)",
        "shell-panel": "rgb(var(--color-shell-panel) / <alpha-value>)",
        "shell-subtle": "rgb(var(--color-shell-subtle) / <alpha-value>)",
        "shell-code": "rgb(var(--color-shell-code) / <alpha-value>)",
        "shell-code-text": "rgb(var(--color-shell-code-text) / <alpha-value>)",
        "shell-border": "rgb(var(--color-shell-border) / <alpha-value>)",
        "shell-muted": "rgb(var(--color-shell-muted) / <alpha-value>)",
        "shell-soft": "rgb(var(--color-shell-soft) / <alpha-value>)",
        "shell-accent": "rgb(var(--color-shell-accent) / <alpha-value>)",
        "shell-accent-text": "rgb(var(--color-shell-accent-text) / <alpha-value>)",
        "shell-warning": "rgb(var(--color-shell-warning) / <alpha-value>)",
        "shell-warning-text": "rgb(var(--color-shell-warning-text) / <alpha-value>)",
        "shell-danger": "rgb(var(--color-shell-danger) / <alpha-value>)",
        "shell-danger-text": "rgb(var(--color-shell-danger-text) / <alpha-value>)",
        "shell-success": "rgb(var(--color-shell-success) / <alpha-value>)",
        "shell-success-text": "rgb(var(--color-shell-success-text) / <alpha-value>)",
      },
      backgroundImage: {
        "mesh-shell": "var(--shell-background)",
      },
      fontFamily: {
        body: ["var(--font-body)"],
        display: ["var(--font-display)"],
      },
    },
  },
  plugins: [],
};
