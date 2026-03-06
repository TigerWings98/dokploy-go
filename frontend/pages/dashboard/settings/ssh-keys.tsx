import type { ReactElement } from "react";
import { ShowDestinations } from "@/components/dashboard/settings/ssh-keys/show-ssh-keys";
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
	return <DashboardLayout metaName="SSH Keys">{page}</DashboardLayout>;
};
