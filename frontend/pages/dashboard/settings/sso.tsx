import type { ReactElement } from "react";
import { DashboardLayout } from "@/components/layouts/dashboard-layout";
import { EnterpriseFeatureGate } from "@/components/proprietary/enterprise-feature-gate";
import { SSOSettings } from "@/components/proprietary/sso/sso-settings";
import { Card } from "@/components/ui/card";

const Page = () => {
	return (
		<div className="w-full">
			<div className="h-full rounded-xl max-w-5xl mx-auto flex flex-col gap-4">
				<Card className="h-full bg-sidebar p-2.5 rounded-xl mx-auto w-full">
					<div className="rounded-xl bg-background shadow-md">
						<div className="p-6">
							<EnterpriseFeatureGate
								lockedProps={{
									title: "Enterprise SSO",
									description:
										"Single sign-on (SSO) with OIDC and SAML is part of Dokploy Enterprise. Add a valid license to configure it.",
									ctaLabel: "Go to License",
								}}
							>
								<SSOSettings />
							</EnterpriseFeatureGate>
						</div>
					</div>
				</Card>
			</div>
		</div>
	);
};

export default Page;

Page.getLayout = (page: ReactElement) => {
	return <DashboardLayout metaName="SSO">{page}</DashboardLayout>;
};
