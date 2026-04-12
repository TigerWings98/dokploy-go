import { useRouter } from "next/router";
import { useEffect, type ReactElement } from "react";
import { ShowContainers } from "@/components/dashboard/docker/show/show-containers";
import { DashboardLayout } from "@/components/layouts/dashboard-layout";
import { api } from "@/utils/api";

const Dashboard = () => {
	const router = useRouter();
	// 与 TS v0.28.7 对齐：使用 getPermissions 代替 canAccessToDocker
	const { data: permissions, isPending } = api.user.getPermissions.useQuery();

	useEffect(() => {
		if (isPending) return;
		if (!permissions?.docker.read) {
			router.replace("/");
		}
	}, [permissions, isPending, router]);

	if (isPending || !permissions?.docker.read) return null;

	return <ShowContainers />;
};

export default Dashboard;

Dashboard.getLayout = (page: ReactElement) => {
	return <DashboardLayout>{page}</DashboardLayout>;
};
