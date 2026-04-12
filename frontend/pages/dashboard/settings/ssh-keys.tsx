import { useRouter } from "next/router";
import { useEffect, type ReactElement } from "react";
import { ShowDestinations } from "@/components/dashboard/settings/ssh-keys/show-ssh-keys";
import { DashboardLayout } from "@/components/layouts/dashboard-layout";
import { api } from "@/utils/api";

const Page = () => {
	const router = useRouter();
	// 与 TS v0.28.7 对齐：使用 getPermissions 代替 canAccessToSSHKeys
	const { data: permissions, isPending } = api.user.getPermissions.useQuery();

	useEffect(() => {
		if (isPending) return;
		if (!permissions?.sshKeys.read) {
			router.replace("/");
		}
	}, [permissions, isPending, router]);

	if (isPending || !permissions?.sshKeys.read) return null;

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
