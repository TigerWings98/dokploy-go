import type { ReactElement } from "react";
import { ShowRegistry } from "@/components/dashboard/settings/cluster/registry/show-registry";
import { DashboardLayout } from "@/components/layouts/dashboard-layout";

const Page = () => {
	return (
		<div className="flex flex-col gap-4 w-full">
			<ShowRegistry />
		</div>
	);
};

export default Page;

Page.getLayout = (page: ReactElement) => {
	return <DashboardLayout metaName="Registry">{page}</DashboardLayout>;
};
