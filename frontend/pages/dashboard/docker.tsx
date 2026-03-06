import type { ReactElement } from "react";
import { ShowContainers } from "@/components/dashboard/docker/show/show-containers";
import { DashboardLayout } from "@/components/layouts/dashboard-layout";

const Dashboard = () => {
	return <ShowContainers />;
};

export default Dashboard;

Dashboard.getLayout = (page: ReactElement) => {
	return <DashboardLayout>{page}</DashboardLayout>;
};
