const nextConfig = {
  reactStrictMode: true,
  output: 'standalone',
  async redirects() {
    return [
      {
        source: '/',
        destination: '/compute/vms',
        permanent: false,
      },
    ];
  },
};

export default nextConfig;
