import type { ReactElement } from "react";
import { ShowApiKeys } from "@/components/dashboard/settings/api/show-api-keys";
import { LinkingAccount } from "@/components/dashboard/settings/linking-account/linking-account";
import { ProfileForm } from "@/components/dashboard/settings/profile/profile-form";
import { DashboardLayout } from "@/components/layouts/dashboard-layout";
import { api } from "@/utils/api";

const Page = () => {
	const { data } = api.user.get.useQuery();
	const { data: isCloud } = api.settings.isCloud.useQuery();

	return (
		<div className="w-full">
			<div className="h-full rounded-xl max-w-5xl mx-auto flex flex-col gap-4">
				<ProfileForm />
				{isCloud && <LinkingAccount />}
				{(data?.canAccessToAPI ||
					data?.role === "owner" ||
					data?.role === "admin") && <ShowApiKeys />}
			</div>
		</div>
	);
};

export default Page;

Page.getLayout = (page: ReactElement) => {
	return <DashboardLayout metaName="Profile">{page}</DashboardLayout>;
};
