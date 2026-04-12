import { useRouter } from "next/router";
import { useEffect, type ReactElement } from "react";
import { ShowTraefikSystem } from "@/components/dashboard/file-system/show-traefik-system";
import { DashboardLayout } from "@/components/layouts/dashboard-layout";
import { api } from "@/utils/api";

const Dashboard = () => {
	const router = useRouter();
	// 与 TS v0.28.7 对齐：使用 getPermissions 代替 canAccessToTraefikFiles
	const { data: permissions, isPending } = api.user.getPermissions.useQuery();

	useEffect(() => {
		if (isPending) return;
		if (!permissions?.traefikFiles.read) {
			router.replace("/");
		}
	}, [permissions, isPending, router]);

	if (isPending || !permissions?.traefikFiles.read) return null;

	return <ShowTraefikSystem />;
};

export default Dashboard;

Dashboard.getLayout = (page: ReactElement) => {
	return <DashboardLayout>{page}</DashboardLayout>;
};
