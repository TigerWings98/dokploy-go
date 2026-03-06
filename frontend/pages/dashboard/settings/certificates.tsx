import type { ReactElement } from "react";
import { ShowCertificates } from "@/components/dashboard/settings/certificates/show-certificates";
import { DashboardLayout } from "@/components/layouts/dashboard-layout";

const Page = () => {
	return (
		<div className="flex flex-col gap-4 w-full">
			<ShowCertificates />
		</div>
	);
};

export default Page;

Page.getLayout = (page: ReactElement) => {
	return <DashboardLayout metaName="Certificates">{page}</DashboardLayout>;
};
