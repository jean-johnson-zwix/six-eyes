import type { Config } from 'tailwindcss';

const config: Config = {
  content: [
    './app/**/*.{ts,tsx}',
    './components/**/*.{ts,tsx}',
  ],
  theme: {
    extend: {
      colors: {
        bg:      '#101010',
        surface: '#181818',
        card:    '#1e1e1e',
        border:  '#2a2a2a',
        text:    '#f5edd6',
        mid:     '#c8b78a',
        dim:     '#7a6a45',
        hype:    '#e8513a',
        likely:  '#f5c842',
        low:     '#3a8a82',
      },
      fontFamily: {
        mono: ['var(--font-mono)', 'Space Mono', 'monospace'],
      },
    },
  },
  plugins: [],
};

export default config;
