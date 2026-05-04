'use client';

import Link from 'next/link';
import { usePathname } from 'next/navigation';

const LINKS = [
  { href: '/',      label: 'Feed' },
  { href: '/admin', label: 'Admin' },
];

export default function Nav() {
  const path = usePathname();

  return (
    <nav className="flex-shrink-0 border-b border-border bg-surface px-6 py-3 flex items-center justify-between">
      <Link
        href="/"
        className="font-bold text-text tracking-tight hover:text-mid transition-colors"
        style={{ fontSize: '1.1rem', letterSpacing: '-0.5px' }}
      >
        <span className="text-dim mr-2" style={{ fontSize: '0.85rem' }}>六</span>
        six-eyes
      </Link>

      <div className="flex items-center gap-6 text-sm">
        {LINKS.map(({ href, label }) => {
          const active = href === '/' ? path === '/' : path.startsWith(href);
          return (
            <Link
              key={href}
              href={href}
              className={`transition-colors ${
                active ? 'text-text' : 'text-dim hover:text-mid'
              }`}
            >
              {label}
            </Link>
          );
        })}
      </div>
    </nav>
  );
}
