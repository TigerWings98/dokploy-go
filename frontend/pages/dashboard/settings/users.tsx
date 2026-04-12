import { useRouter } from "next/router";
import { useEffect, type ReactElement } from "react";
import { ShowInvitations } from "@/components/dashboard/settings/users/show-invitations";
import { ShowUsers } from "@/components/dashboard/settings/users/show-users";
import { DashboardLayout } from "@/components/layouts/dashboard-layout";
// 与 TS v0.28.7 对齐：proprietary/roles/manage-custom-roles (enterprise)
// frontend/ 版本没有这个组件，跳过渲染
import { api } from "@/utils/api";

const Page = () => {
	const router = useRouter();
	const { data: auth } = api.user.get.useQuery();
	const { data: permissions, isPending } = api.user.getPermissions.useQuery();
	const isOwnerOrAdmin = auth?.role === "owner" || auth?.role === "admin";
	const canCreateMembers = permissions?.member.create ?? false;

	useEffect(() => {
		if (isPending) return;
		if (!permissions?.member.read) {
			router.replace("/");
		}
	}, [permissions, isPending, router]);

	if (isPending || !permissions?.member.read) return null;

	return (
		<div className="flex flex-col gap-4 w-full">
			<ShowUsers />
			{canCreateMembers && <ShowInvitations />}
			{isOwnerOrAdmin && (
				<div className="text-sm text-muted-foreground text-center py-4">
					Custom roles (enterprise) are not available in this build.
				</div>
			)}
		</div>
	);
};

export default Page;

Page.getLayout = (page: ReactElement) => {
	return <DashboardLayout metaName="Users">{page}</DashboardLayout>;
};
