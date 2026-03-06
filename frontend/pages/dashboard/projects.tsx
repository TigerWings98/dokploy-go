import dynamic from "next/dynamic";
import type { ReactElement } from "react";
import { ShowProjects } from "@/components/dashboard/projects/show";
import { DashboardLayout } from "@/components/layouts/dashboard-layout";
import { api } from "@/utils/api";

const ShowWelcomeDokploy = dynamic(
	() =>
		import("@/components/dashboard/settings/billing/show-welcome-dokploy").then(
			(mod) => mod.ShowWelcomeDokploy,
		),
	{ ssr: false },
);

const Dashboard = () => {
	const { data: isCloud } = api.settings.isCloud.useQuery();
	return (
		<>
			{isCloud && <ShowWelcomeDokploy />}

			<ShowProjects />
		</>
	);
};

export default Dashboard;

Dashboard.getLayout = (page: ReactElement) => {
	return <DashboardLayout>{page}</DashboardLayout>;
};
