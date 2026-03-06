import type { ReactElement } from "react";
import { ShowDestinations } from "@/components/dashboard/settings/destination/show-destinations";
import { DashboardLayout } from "@/components/layouts/dashboard-layout";

const Page = () => {
	return (
		<div className="flex flex-col gap-4 w-full">
			<ShowDestinations />
		</div>
	);
};

export default Page;

Page.getLayout = (page: ReactElement) => {
	return <DashboardLayout metaName="S3 Destinations">{page}</DashboardLayout>;
};
