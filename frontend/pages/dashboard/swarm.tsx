import type { ReactElement } from "react";
import SwarmMonitorCard from "@/components/dashboard/swarm/monitoring-card";
import { DashboardLayout } from "@/components/layouts/dashboard-layout";

const Dashboard = () => {
	return <SwarmMonitorCard />;
};

export default Dashboard;

Dashboard.getLayout = (page: ReactElement) => {
	return <DashboardLayout>{page}</DashboardLayout>;
};
