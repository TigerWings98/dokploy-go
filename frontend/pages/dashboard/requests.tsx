import type { ReactElement } from "react";
import { ShowRequests } from "@/components/dashboard/requests/show-requests";
import { DashboardLayout } from "@/components/layouts/dashboard-layout";

export default function Requests() {
	return <ShowRequests />;
}
Requests.getLayout = (page: ReactElement) => {
	return <DashboardLayout>{page}</DashboardLayout>;
};
