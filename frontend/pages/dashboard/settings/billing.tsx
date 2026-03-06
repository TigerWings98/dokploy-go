import type { ReactElement } from "react";
import { ShowBilling } from "@/components/dashboard/settings/billing/show-billing";
import { DashboardLayout } from "@/components/layouts/dashboard-layout";

const Page = () => {
	return <ShowBilling />;
};

export default Page;

Page.getLayout = (page: ReactElement) => {
	return <DashboardLayout metaName="Billing">{page}</DashboardLayout>;
};
