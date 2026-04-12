import { useRouter } from "next/router";
import { useEffect, type ReactElement } from "react";
import { ShowGitProviders } from "@/components/dashboard/settings/git/show-git-providers";
import { DashboardLayout } from "@/components/layouts/dashboard-layout";
import { api } from "@/utils/api";

const Page = () => {
	const router = useRouter();
	// 与 TS v0.28.7 对齐：使用 getPermissions 代替 canAccessToGitProviders
	const { data: permissions, isPending } = api.user.getPermissions.useQuery();

	useEffect(() => {
		if (isPending) return;
		if (!permissions?.gitProviders.read) {
			router.replace("/");
		}
	}, [permissions, isPending, router]);

	if (isPending || !permissions?.gitProviders.read) return null;

	return (
		<div className="flex flex-col gap-4 w-full">
			<ShowGitProviders />
		</div>
	);
};

export default Page;

Page.getLayout = (page: ReactElement) => {
	return <DashboardLayout metaName="Git Providers">{page}</DashboardLayout>;
};
