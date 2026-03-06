import type { ReactElement } from "react";
import { ShowServers } from "@/components/dashboard/settings/servers/show-servers";
import { DashboardLayout } from "@/components/layouts/dashboard-layout";

const Page = () => {
	return (
		<div className="flex flex-col gap-4 w-full">
			<ShowServers />
		</div>
	);
};

export default Page;

Page.getLayout = (page: ReactElement) => {
	return <DashboardLayout metaName="Servers">{page}</DashboardLayout>;
};
