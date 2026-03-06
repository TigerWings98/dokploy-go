import type { ReactElement } from "react";
import { ShowNotifications } from "@/components/dashboard/settings/notifications/show-notifications";
import { DashboardLayout } from "@/components/layouts/dashboard-layout";

const Page = () => {
	return (
		<div className="flex flex-col gap-4 w-full">
			<ShowNotifications />
		</div>
	);
};

export default Page;

Page.getLayout = (page: ReactElement) => {
	return <DashboardLayout metaName="Notifications">{page}</DashboardLayout>;
};
