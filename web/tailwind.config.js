/** @type {import('tailwindcss').Config} */
export default {
  content: ['./index.html', './src/**/*.{ts,tsx}'],
  theme: {
    extend: {
      colors: {
        ink: {
          900: '#0a0e14',
          800: '#0f141c',
          700: '#161c26',
          600: '#1d2530',
          500: '#2a3340',
        },
        accent: {
          DEFAULT: '#4dd0e1',
          glow: '#5cf2ff',
        },
        ok: '#34d399',
        warn: '#fbbf24',
        bad: '#fb7185',
      },
      fontFamily: {
        mono: ['ui-monospace', 'SFMono-Regular', 'Menlo', 'Consolas', 'monospace'],
      },
      boxShadow: {
        glow: '0 0 24px -4px rgba(92,242,255,0.45)',
      },
      keyframes: {
        pulseGlow: {
          '0%,100%': { opacity: '0.6' },
          '50%': { opacity: '1' },
        },
      },
      animation: {
        pulseGlow: 'pulseGlow 1.6s ease-in-out infinite',
      },
    },
  },
  plugins: [],
}
