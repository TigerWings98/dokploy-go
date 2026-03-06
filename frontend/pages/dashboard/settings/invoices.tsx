import type { ReactElement } from "react";
import { ShowBillingInvoices } from "@/components/dashboard/settings/billing/show-billing-invoices";
import { DashboardLayout } from "@/components/layouts/dashboard-layout";

const Page = () => {
	return <ShowBillingInvoices />;
};

export default Page;

Page.getLayout = (page: ReactElement) => {
	return <DashboardLayout metaName="Invoices">{page}</DashboardLayout>;
};
