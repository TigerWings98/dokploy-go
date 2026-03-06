import { useRouter } from "next/router";
import { useEffect, useState } from "react";

/**
 * Custom 404 page that acts as SPA fallback for dynamic routes.
 *
 * Flow:
 * 1. User hits a dynamic route like /dashboard/project/abc123/environment/env456
 * 2. Go server can't find a static file, serves 404.html
 * 3. This page loads, attempts client-side navigation via router.replace()
 * 4. If Next.js client router matches, the correct page renders
 * 5. If it doesn't match (routeChangeComplete fires back to /404), show real 404
 */

const KNOWN_ROUTE_PREFIXES = [
	"/dashboard/project/",
	"/dashboard/settings/",
	"/invitation/",
];

function isLikelyDynamicRoute(path: string): boolean {
	return KNOWN_ROUTE_PREFIXES.some((prefix) => path.startsWith(prefix));
}

export default function Custom404() {
	const router = useRouter();
	const [showNotFound, setShowNotFound] = useState(false);

	useEffect(() => {
		const path = window.location.pathname + window.location.search;

		// Already on /404 explicitly, show 404 immediately
		if (path === "/404") {
			setShowNotFound(true);
			return;
		}

		// For known dynamic route patterns, try client-side navigation
		if (isLikelyDynamicRoute(path)) {
			// Listen for route change to detect if navigation succeeded
			const handleComplete = (url: string) => {
				// If we ended up back at /404, the route doesn't exist
				if (url === "/404") {
					setShowNotFound(true);
				}
			};
			router.events.on("routeChangeComplete", handleComplete);
			router.replace(path);

			return () => {
				router.events.off("routeChangeComplete", handleComplete);
			};
		}

		// Unknown path pattern — show 404 after a brief attempt
		// Still try navigation in case we missed a valid route
		const timeout = setTimeout(() => {
			setShowNotFound(true);
		}, 2000);

		router.replace(path);

		const handleComplete = (url: string) => {
			if (url === "/404") {
				clearTimeout(timeout);
				setShowNotFound(true);
			} else {
				clearTimeout(timeout);
			}
		};
		router.events.on("routeChangeComplete", handleComplete);

		return () => {
			clearTimeout(timeout);
			router.events.off("routeChangeComplete", handleComplete);
		};
	}, [router]);

	if (showNotFound) {
		return (
			<div className="flex flex-col items-center justify-center min-h-screen gap-4">
				<h1 className="text-6xl font-bold text-muted-foreground">404</h1>
				<p className="text-lg text-muted-foreground">Page not found</p>
				<button
					onClick={() => router.push("/dashboard/projects")}
					className="mt-4 px-4 py-2 rounded-md bg-primary text-primary-foreground hover:bg-primary/90 transition-colors"
				>
					Go to Dashboard
				</button>
			</div>
		);
	}

	return (
		<div className="flex items-center justify-center min-h-screen">
			<div className="animate-spin rounded-full h-8 w-8 border-b-2 border-gray-900 dark:border-gray-100" />
		</div>
	);
}
