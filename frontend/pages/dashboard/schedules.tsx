import type { ReactElement } from "react";
import { ShowSchedules } from "@/components/dashboard/application/schedules/show-schedules";
import { DashboardLayout } from "@/components/layouts/dashboard-layout";
import { Card } from "@/components/ui/card";
import { api } from "@/utils/api";

function SchedulesPage() {
	const { data: user } = api.user.get.useQuery();
	return (
		<div className="w-full">
			<Card className="h-full bg-sidebar  p-2.5 rounded-xl  max-w-8xl mx-auto min-h-[45vh]">
				<div className="rounded-xl bg-background shadow-md h-full">
					<ShowSchedules
						scheduleType="dokploy-server"
						id={user?.user.id || ""}
					/>
				</div>
			</Card>
		</div>
	);
}
export default SchedulesPage;

SchedulesPage.getLayout = (page: ReactElement) => {
	return <DashboardLayout>{page}</DashboardLayout>;
};
