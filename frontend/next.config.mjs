/**
 * Run `build` or `dev` with `SKIP_ENV_VALIDATION` to skip env validation. This is especially useful
 * for Docker builds.
 */

/** @type {import("next").NextConfig} */
const nextConfig = {
	output: "export",
	reactStrictMode: true,
	typescript: {
		ignoreBuildErrors: true,
	},
	images: {
		unoptimized: true,
	},
	// transpilePackages removed: @dokploy/server is only used via type imports now
	// eslint removed: not supported in Next.js 16
	// Security headers are handled by the Go server instead
};

export default nextConfig;
