import type { Config } from "tailwindcss";

const config: Config = {
  content: [
    "./src/pages/**/*.{js,ts,jsx,tsx,mdx}",
    "./src/components/**/*.{js,ts,jsx,tsx,mdx}",
    "./src/app/**/*.{js,ts,jsx,tsx,mdx}",
    // Shared design tokens (hazard badge, cluster danger ring) are string literals
    // in these modules and are consumed by dynamic className expressions, so they
    // must be scanned or the classes are purged (S14-T3 / Issue #443).
    "./src/utils/**/*.{js,ts}",
    "./src/config/**/*.{js,ts}",
  ],
  theme: {
    extend: {
      colors: {
        background: "var(--background)",
        foreground: "var(--foreground)",
      },
    },
  },
  plugins: [],
};
export default config;
