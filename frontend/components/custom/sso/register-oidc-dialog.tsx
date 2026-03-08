import { useState } from "react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import {
	Dialog,
	DialogContent,
	DialogDescription,
	DialogFooter,
	DialogHeader,
	DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { api } from "@/utils/api";

interface Props {
	open: boolean;
	onOpenChange: (open: boolean) => void;
}

export function RegisterOIDCDialog({ open, onOpenChange }: Props) {
	const utils = api.useUtils();
	const [isLoading, setIsLoading] = useState(false);
	const [form, setForm] = useState({
		providerId: "",
		issuer: "",
		clientId: "",
		clientSecret: "",
		domains: "",
	});

	const registerProvider = api.sso.register.useMutation({
		onSuccess: () => {
			toast.success("OIDC provider registered");
			utils.sso.listProviders.invalidate();
			onOpenChange(false);
			setForm({
				providerId: "",
				issuer: "",
				clientId: "",
				clientSecret: "",
				domains: "",
			});
		},
		onError: (err) => toast.error(err.message),
	});

	const handleSubmit = (e: React.FormEvent) => {
		e.preventDefault();
		if (!form.providerId || !form.issuer || !form.clientId) {
			toast.error("Provider ID, Issuer, and Client ID are required");
			return;
		}
		registerProvider.mutate({
			providerId: form.providerId,
			issuer: form.issuer,
			domains: form.domains
				.split(",")
				.map((d) => d.trim())
				.filter(Boolean),
			oidcConfig: JSON.stringify({
				clientId: form.clientId,
				clientSecret: form.clientSecret,
				scopes: ["openid", "profile", "email"],
			}),
		});
	};

	return (
		<Dialog open={open} onOpenChange={onOpenChange}>
			<DialogContent className="max-w-md">
				<DialogHeader>
					<DialogTitle>Register OIDC Provider</DialogTitle>
					<DialogDescription>
						Configure an OpenID Connect identity provider for SSO login.
					</DialogDescription>
				</DialogHeader>
				<form onSubmit={handleSubmit} className="space-y-4">
					<div className="space-y-2">
						<Label>Provider ID</Label>
						<Input
							placeholder="e.g. logto, okta, auth0"
							value={form.providerId}
							onChange={(e) =>
								setForm({ ...form, providerId: e.target.value })
							}
						/>
						<p className="text-xs text-muted-foreground">
							A unique identifier for this provider
						</p>
					</div>
					<div className="space-y-2">
						<Label>Issuer URL</Label>
						<Input
							placeholder="https://auth.example.com"
							value={form.issuer}
							onChange={(e) =>
								setForm({ ...form, issuer: e.target.value })
							}
						/>
						<p className="text-xs text-muted-foreground">
							The OIDC issuer URL (supports auto-discovery)
						</p>
					</div>
					<div className="space-y-2">
						<Label>Client ID</Label>
						<Input
							placeholder="Client ID from your IdP"
							value={form.clientId}
							onChange={(e) =>
								setForm({ ...form, clientId: e.target.value })
							}
						/>
					</div>
					<div className="space-y-2">
						<Label>Client Secret</Label>
						<Input
							type="password"
							placeholder="Client Secret from your IdP"
							value={form.clientSecret}
							onChange={(e) =>
								setForm({ ...form, clientSecret: e.target.value })
							}
						/>
					</div>
					<div className="space-y-2">
						<Label>Domains</Label>
						<Input
							placeholder="example.com, corp.example.com"
							value={form.domains}
							onChange={(e) =>
								setForm({ ...form, domains: e.target.value })
							}
						/>
						<p className="text-xs text-muted-foreground">
							Comma-separated email domains that use this provider
						</p>
					</div>
					<DialogFooter>
						<Button
							variant="outline"
							type="button"
							onClick={() => onOpenChange(false)}
						>
							Cancel
						</Button>
						<Button
							type="submit"
							isLoading={registerProvider.isPending}
						>
							Register
						</Button>
					</DialogFooter>
				</form>
			</DialogContent>
		</Dialog>
	);
}
