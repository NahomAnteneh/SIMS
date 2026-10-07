const queryApiUrl =
  process.env.INTERNAL_QUERY_API_URL ||
  process.env.QUERY_API_URL ||
  process.env.NEXT_PUBLIC_QUERY_API_URL ||
  "http://localhost:8080";

const nextConfig = {
  reactStrictMode: true,
  output: 'standalone',
  async redirects() {
    return [
      {
        source: '/',
        destination: '/dashboard',
        permanent: false,
      },
    ];
  },
  async rewrites() {
    return [
      {
        source: '/api/v1/:path*',
        destination: `${queryApiUrl}/v1/:path*`,
      },
    ];
  },
};

export default nextConfig;
