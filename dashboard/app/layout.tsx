import type { Metadata } from 'next';
import { Space_Mono } from 'next/font/google';
import './globals.css';
import Nav from '@/components/Nav';

const spaceMono = Space_Mono({
  subsets: ['latin'],
  weight: ['400', '700'],
  variable: '--font-mono',
});

export const metadata: Metadata = {
  title: 'six-eyes',
  description: 'Arxiv ML paper hype predictor — ranked feed updated daily',
  icons: { icon: '/favicon.ico' },
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en" className={spaceMono.variable}>
      <body className="font-mono bg-bg text-text flex flex-col min-h-screen">
        {/* Rainbow accent bar */}
        <div
          className="h-[5px] flex-shrink-0 w-full"
          style={{
            background:
              'linear-gradient(90deg,#e8513a,#f09030,#f5c842,#5a9e52,#3a8a82,#4a72c4,#9068c0,#d44878)',
          }}
        />

        <Nav />

        <main className="flex-1">
          {children}
        </main>

        <footer className="border-t border-border px-6 py-4 text-dim text-xs text-center">
          six-eyes · papers ranked by XGBoost hype model · updated daily
        </footer>
      </body>
    </html>
  );
}
