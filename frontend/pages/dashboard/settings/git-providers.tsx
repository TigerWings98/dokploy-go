import type { ReactElement } from "react";
import { ShowGitProviders } from "@/components/dashboard/settings/git/show-git-providers";
import { DashboardLayout } from "@/components/layouts/dashboard-layout";

const Page = () => {
	return (
		<div className="flex flex-col gap-4 w-full">
			<ShowGitProviders />
		</div>
	);
};

export default Page;

Page.getLayout = (page: ReactElement) => {
	return <DashboardLayout metaName="Git Providers">{page}</DashboardLayout>;
};
