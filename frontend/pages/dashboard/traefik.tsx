import type { ReactElement } from "react";
import { ShowTraefikSystem } from "@/components/dashboard/file-system/show-traefik-system";
import { DashboardLayout } from "@/components/layouts/dashboard-layout";

const Dashboard = () => {
	return <ShowTraefikSystem />;
};

export default Dashboard;

Dashboard.getLayout = (page: ReactElement) => {
	return <DashboardLayout>{page}</DashboardLayout>;
};
