import type { NextConfig } from 'next';

const nextConfig: NextConfig = {
  // Allow the Go API origin for server-side fetches
  // (no browser CORS needed — all fetches are server-side)
};

export default nextConfig;
