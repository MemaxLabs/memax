import { createMDX } from "fumadocs-mdx/next";

const withMDX = createMDX();

export default withMDX({
  reactStrictMode: true,
  async redirects() {
    return [
      {
        source: "/benchmarks",
        destination: "/quickstart/benchmarks",
        permanent: true,
      },
    ];
  },
});
