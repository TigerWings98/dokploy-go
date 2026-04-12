import { useRouter } from "next/router";
import { useEffect, type ReactElement } from "react";
import SwarmMonitorCard from "@/components/dashboard/swarm/monitoring-card";
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

	return <SwarmMonitorCard />;
};

export default Dashboard;

Dashboard.getLayout = (page: ReactElement) => {
	return <DashboardLayout>{page}</DashboardLayout>;
};
