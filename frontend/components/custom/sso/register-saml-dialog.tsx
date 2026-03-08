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
import { Textarea } from "@/components/ui/textarea";
import { api } from "@/utils/api";

interface Props {
	open: boolean;
	onOpenChange: (open: boolean) => void;
}

export function RegisterSAMLDialog({ open, onOpenChange }: Props) {
	const utils = api.useUtils();
	const [form, setForm] = useState({
		providerId: "",
		issuer: "",
		entryPoint: "",
		cert: "",
		domains: "",
	});

	const registerProvider = api.sso.register.useMutation({
		onSuccess: () => {
			toast.success("SAML provider registered");
			utils.sso.listProviders.invalidate();
			onOpenChange(false);
			setForm({
				providerId: "",
				issuer: "",
				entryPoint: "",
				cert: "",
				domains: "",
			});
		},
		onError: (err) => toast.error(err.message),
	});

	const handleSubmit = (e: React.FormEvent) => {
		e.preventDefault();
		if (!form.providerId || !form.issuer || !form.entryPoint) {
			toast.error(
				"Provider ID, Issuer, and Entry Point are required",
			);
			return;
		}
		registerProvider.mutate({
			providerId: form.providerId,
			issuer: form.issuer,
			domains: form.domains
				.split(",")
				.map((d) => d.trim())
				.filter(Boolean),
			samlConfig: JSON.stringify({
				entryPoint: form.entryPoint,
				cert: form.cert,
			}),
		});
	};

	return (
		<Dialog open={open} onOpenChange={onOpenChange}>
			<DialogContent className="max-w-md">
				<DialogHeader>
					<DialogTitle>Register SAML Provider</DialogTitle>
					<DialogDescription>
						Configure a SAML 2.0 identity provider for SSO login.
					</DialogDescription>
				</DialogHeader>
				<form onSubmit={handleSubmit} className="space-y-4">
					<div className="space-y-2">
						<Label>Provider ID</Label>
						<Input
							placeholder="e.g. okta-saml, adfs"
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
						<Label>Issuer / Entity ID</Label>
						<Input
							placeholder="https://idp.example.com/saml/metadata"
							value={form.issuer}
							onChange={(e) =>
								setForm({ ...form, issuer: e.target.value })
							}
						/>
					</div>
					<div className="space-y-2">
						<Label>SSO Entry Point URL</Label>
						<Input
							placeholder="https://idp.example.com/saml/sso"
							value={form.entryPoint}
							onChange={(e) =>
								setForm({ ...form, entryPoint: e.target.value })
							}
						/>
						<p className="text-xs text-muted-foreground">
							The IdP&apos;s SSO login URL
						</p>
					</div>
					<div className="space-y-2">
						<Label>IdP Certificate (PEM)</Label>
						<Textarea
							placeholder="-----BEGIN CERTIFICATE-----&#10;...&#10;-----END CERTIFICATE-----"
							rows={4}
							value={form.cert}
							onChange={(e) =>
								setForm({ ...form, cert: e.target.value })
							}
						/>
						<p className="text-xs text-muted-foreground">
							The IdP&apos;s X.509 signing certificate in PEM format
						</p>
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
