import { useRouter } from "next/router";
import { useEffect, type ReactNode } from "react";
import { authClient } from "@/lib/auth-client";

interface AuthGuardProps {
	children: ReactNode;
	requireAuth?: boolean;
	redirectTo?: string;
}

/**
 * Client-side auth guard. Replaces server-side validateRequest + redirect.
 * - requireAuth=true (default): redirect to "/" if not logged in
 * - requireAuth=false: redirect to "/dashboard/projects" if already logged in (for login/register pages)
 */
export function AuthGuard({
	children,
	requireAuth = true,
	redirectTo,
}: AuthGuardProps) {
	const router = useRouter();
	const { data: session, isPending } = authClient.useSession();

	useEffect(() => {
		if (isPending) return;

		if (requireAuth && !session?.user) {
			router.replace(redirectTo || "/");
		} else if (!requireAuth && session?.user) {
			router.replace(redirectTo || "/dashboard/projects");
		}
	}, [session, isPending, requireAuth, redirectTo, router]);

	if (isPending) {
		return (
			<div className="flex items-center justify-center min-h-[50vh]">
				<div className="animate-spin rounded-full h-8 w-8 border-b-2 border-primary" />
			</div>
		);
	}

	if (requireAuth && !session?.user) {
		return null;
	}

	if (!requireAuth && session?.user) {
		return null;
	}

	return <>{children}</>;
}
