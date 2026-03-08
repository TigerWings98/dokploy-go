import { Settings2 } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import {
	Card,
	CardContent,
	CardDescription,
	CardHeader,
	CardTitle,
} from "@/components/ui/card";
import { Switch } from "@/components/ui/switch";
import { Label } from "@/components/ui/label";
import { api } from "@/utils/api";

export function LicenseKeySettings() {
	const utils = api.useUtils();
	const { data: settings, isLoading } =
		api.licenseKey.getEnterpriseSettings.useQuery();

	const updateSettings =
		api.licenseKey.updateEnterpriseSettings.useMutation({
			onSuccess: () => {
				toast.success("Settings updated");
				utils.licenseKey.getEnterpriseSettings.invalidate();
				utils.licenseKey.haveValidLicenseKey.invalidate();
			},
			onError: (err) => toast.error(err.message),
		});

	return (
		<div className="space-y-6">
			<div>
				<h3 className="text-lg font-medium">Enterprise Features</h3>
				<p className="text-sm text-muted-foreground">
					Toggle enterprise features for your self-hosted instance
				</p>
			</div>

			<Card>
				<CardHeader>
					<CardTitle className="text-base flex items-center gap-2">
						<Settings2 className="h-4 w-4" />
						Feature Toggle
					</CardTitle>
					<CardDescription>
						Enable or disable enterprise features like SSO. Since this
						is a self-hosted Go rewrite, no license key is required.
					</CardDescription>
				</CardHeader>
				<CardContent>
					{isLoading ? (
						<p className="text-sm text-muted-foreground">Loading...</p>
					) : (
						<div className="flex items-center justify-between">
							<div className="space-y-0.5">
								<Label>Enable Enterprise Features</Label>
								<p className="text-xs text-muted-foreground">
									Enables SSO and other enterprise functionality
								</p>
							</div>
							<Switch
								checked={
									settings?.enableEnterpriseFeatures ?? false
								}
								onCheckedChange={(checked) => {
									updateSettings.mutate({
										enableEnterpriseFeatures: checked,
									});
								}}
							/>
						</div>
					)}
				</CardContent>
			</Card>
		</div>
	);
}
