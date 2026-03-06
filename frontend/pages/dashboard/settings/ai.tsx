import type { ReactElement } from "react";
import { AiForm } from "@/components/dashboard/settings/ai-form";
import { DashboardLayout } from "@/components/layouts/dashboard-layout";

const Page = () => {
	return (
		<div className="flex flex-col gap-4 w-full">
			<AiForm />
		</div>
	);
};

export default Page;

Page.getLayout = (page: ReactElement) => {
	return <DashboardLayout>{page}</DashboardLayout>;
};
