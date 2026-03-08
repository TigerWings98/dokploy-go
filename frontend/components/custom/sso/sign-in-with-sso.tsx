import { useRouter } from "next/router";
import { type ReactNode, useState } from "react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
	Tabs,
	TabsContent,
	TabsList,
	TabsTrigger,
} from "@/components/ui/tabs";

interface SignInWithSSOProps {
	children: ReactNode;
}

export function SignInWithSSO({ children }: SignInWithSSOProps) {
	const router = useRouter();
	const [ssoEmail, setSsoEmail] = useState("");
	const [isSSOLoading, setIsSSOLoading] = useState(false);

	const handleSSOLogin = async (e: React.FormEvent) => {
		e.preventDefault();
		if (!ssoEmail) {
			toast.error("Please enter your email");
			return;
		}
		setIsSSOLoading(true);
		try {
			const res = await fetch("/api/auth/sign-in/sso", {
				method: "POST",
				headers: { "Content-Type": "application/json" },
				body: JSON.stringify({
					email: ssoEmail,
					callbackURL: `${window.location.origin}/dashboard/projects`,
				}),
			});
			const data = await res.json();
			if (!res.ok) {
				toast.error(data.message || "SSO login failed");
				return;
			}
			if (data.url) {
				window.location.href = data.url;
			}
		} catch {
			toast.error("SSO login failed");
		} finally {
			setIsSSOLoading(false);
		}
	};

	return (
		<Tabs defaultValue="credentials" className="w-full">
			<TabsList className="grid w-full grid-cols-2">
				<TabsTrigger value="credentials">Password</TabsTrigger>
				<TabsTrigger value="sso">SSO</TabsTrigger>
			</TabsList>
			<TabsContent value="credentials" className="space-y-4 mt-4">
				{children}
			</TabsContent>
			<TabsContent value="sso" className="space-y-4 mt-4">
				<form onSubmit={handleSSOLogin} className="space-y-4">
					<div className="space-y-2">
						<Label>Email</Label>
						<Input
							type="email"
							placeholder="you@company.com"
							value={ssoEmail}
							onChange={(e) => setSsoEmail(e.target.value)}
						/>
						<p className="text-xs text-muted-foreground">
							Enter your corporate email to sign in via SSO
						</p>
					</div>
					<Button
						className="w-full"
						type="submit"
						isLoading={isSSOLoading}
					>
						Continue with SSO
					</Button>
				</form>
			</TabsContent>
		</Tabs>
	);
}
