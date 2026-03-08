import { Plus, Settings2, Trash2, Globe, Pencil } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import {
	Card,
	CardContent,
	CardDescription,
	CardHeader,
	CardTitle,
} from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
	AlertDialog,
	AlertDialogAction,
	AlertDialogCancel,
	AlertDialogContent,
	AlertDialogDescription,
	AlertDialogFooter,
	AlertDialogHeader,
	AlertDialogTitle,
	AlertDialogTrigger,
} from "@/components/ui/alert-dialog";
import { Badge } from "@/components/ui/badge";
import { RegisterOIDCDialog } from "./register-oidc-dialog";
import { RegisterSAMLDialog } from "./register-saml-dialog";
import { api } from "@/utils/api";

export function SSOSettings() {
	const utils = api.useUtils();
	const { data: providers = [], isLoading } =
		api.sso.listProviders.useQuery();
	const { data: trustedOrigins = [] } =
		api.sso.getTrustedOrigins.useQuery();

	const deleteProvider = api.sso.deleteProvider.useMutation({
		onSuccess: () => {
			toast.success("Provider deleted");
			utils.sso.listProviders.invalidate();
		},
		onError: (err) => toast.error(err.message),
	});

	const addOrigin = api.sso.addTrustedOrigin.useMutation({
		onSuccess: () => {
			toast.success("Origin added");
			utils.sso.getTrustedOrigins.invalidate();
			setNewOrigin("");
		},
		onError: (err) => toast.error(err.message),
	});

	const removeOrigin = api.sso.removeTrustedOrigin.useMutation({
		onSuccess: () => {
			toast.success("Origin removed");
			utils.sso.getTrustedOrigins.invalidate();
		},
		onError: (err) => toast.error(err.message),
	});

	const [newOrigin, setNewOrigin] = useState("");
	const [showOIDC, setShowOIDC] = useState(false);
	const [showSAML, setShowSAML] = useState(false);

	return (
		<div className="space-y-6">
			<div>
				<h3 className="text-lg font-medium">SSO Configuration</h3>
				<p className="text-sm text-muted-foreground">
					Configure Single Sign-On providers for your organization
				</p>
			</div>

			{/* SSO Providers */}
			<Card>
				<CardHeader className="flex flex-row items-center justify-between">
					<div>
						<CardTitle className="text-base">SSO Providers</CardTitle>
						<CardDescription>
							Manage OIDC and SAML identity providers
						</CardDescription>
					</div>
					<div className="flex gap-2">
						<Button
							variant="outline"
							size="sm"
							onClick={() => setShowOIDC(true)}
						>
							<Plus className="mr-1 h-4 w-4" />
							OIDC
						</Button>
						<Button
							variant="outline"
							size="sm"
							onClick={() => setShowSAML(true)}
						>
							<Plus className="mr-1 h-4 w-4" />
							SAML
						</Button>
					</div>
				</CardHeader>
				<CardContent>
					{isLoading ? (
						<p className="text-sm text-muted-foreground">Loading...</p>
					) : providers.length === 0 ? (
						<p className="text-sm text-muted-foreground">
							No SSO providers configured. Add an OIDC or SAML provider
							to get started.
						</p>
					) : (
						<div className="space-y-3">
							{providers.map((provider: any) => (
								<div
									key={provider.providerId}
									className="flex items-center justify-between rounded-lg border p-3"
								>
									<div className="space-y-1">
										<div className="flex items-center gap-2">
											<Settings2 className="h-4 w-4 text-muted-foreground" />
											<span className="font-medium">
												{provider.providerId}
											</span>
											<Badge variant="secondary">
												{provider.oidcConfig
													? "OIDC"
													: provider.samlConfig
														? "SAML"
														: "Unknown"}
											</Badge>
										</div>
										<p className="text-xs text-muted-foreground">
											{provider.issuer}
										</p>
										{provider.domain && (
											<p className="text-xs text-muted-foreground">
												Domains: {provider.domain}
											</p>
										)}
									</div>
									<AlertDialog>
										<AlertDialogTrigger asChild>
											<Button variant="ghost" size="icon">
												<Trash2 className="h-4 w-4 text-destructive" />
											</Button>
										</AlertDialogTrigger>
										<AlertDialogContent>
											<AlertDialogHeader>
												<AlertDialogTitle>
													Delete SSO Provider
												</AlertDialogTitle>
												<AlertDialogDescription>
													This will remove the SSO provider &ldquo;
													{provider.providerId}&rdquo;. Users will no
													longer be able to sign in with this provider.
												</AlertDialogDescription>
											</AlertDialogHeader>
											<AlertDialogFooter>
												<AlertDialogCancel>Cancel</AlertDialogCancel>
												<AlertDialogAction
													onClick={() =>
														deleteProvider.mutate({
															providerId: provider.providerId,
														})
													}
												>
													Delete
												</AlertDialogAction>
											</AlertDialogFooter>
										</AlertDialogContent>
									</AlertDialog>
								</div>
							))}
						</div>
					)}
				</CardContent>
			</Card>

			{/* Trusted Origins */}
			<Card>
				<CardHeader>
					<CardTitle className="text-base">Trusted Origins</CardTitle>
					<CardDescription>
						Origins allowed for SSO redirects. Add your identity
						provider&apos;s URL here.
					</CardDescription>
				</CardHeader>
				<CardContent className="space-y-4">
					<div className="flex gap-2">
						<Input
							placeholder="https://login.example.com"
							value={newOrigin}
							onChange={(e) => setNewOrigin(e.target.value)}
						/>
						<Button
							variant="outline"
							onClick={() => {
								if (!newOrigin) return;
								addOrigin.mutate({ origin: newOrigin });
							}}
							disabled={!newOrigin}
						>
							<Plus className="mr-1 h-4 w-4" />
							Add
						</Button>
					</div>
					{(trustedOrigins as string[]).length === 0 ? (
						<p className="text-sm text-muted-foreground">
							No trusted origins configured.
						</p>
					) : (
						<div className="space-y-2">
							{(trustedOrigins as string[]).map((origin) => (
								<div
									key={origin}
									className="flex items-center justify-between rounded-lg border p-2 px-3"
								>
									<div className="flex items-center gap-2">
										<Globe className="h-4 w-4 text-muted-foreground" />
										<span className="text-sm">{origin}</span>
									</div>
									<Button
										variant="ghost"
										size="icon"
										onClick={() =>
											removeOrigin.mutate({ origin })
										}
									>
										<Trash2 className="h-4 w-4 text-destructive" />
									</Button>
								</div>
							))}
						</div>
					)}
				</CardContent>
			</Card>

			<RegisterOIDCDialog
				open={showOIDC}
				onOpenChange={setShowOIDC}
			/>
			<RegisterSAMLDialog
				open={showSAML}
				onOpenChange={setShowSAML}
			/>
		</div>
	);
}
