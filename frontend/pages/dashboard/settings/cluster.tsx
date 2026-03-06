import type { ReactElement } from "react";
import { ShowNodes } from "@/components/dashboard/settings/cluster/nodes/show-nodes";
import { DashboardLayout } from "@/components/layouts/dashboard-layout";

const Page = () => {
	return (
		<div className="flex flex-col gap-4 w-full">
			<ShowNodes />
		</div>
	);
};

export default Page;

Page.getLayout = (page: ReactElement) => {
	return <DashboardLayout metaName="Nodes">{page}</DashboardLayout>;
};
