import { standardSchemaResolver as zodResolver } from "@hookform/resolvers/standard-schema";
import { CheckCircle2, Loader2, Settings2, Wifi } from "lucide-react";
import { useEffect, useState } from "react";
import { useForm } from "react-hook-form";
import { toast } from "sonner";
import { z } from "zod";
import { AlertBlock } from "@/components/shared/alert-block";
import { Button } from "@/components/ui/button";
import {
	Dialog,
	DialogContent,
	DialogDescription,
	DialogFooter,
	DialogHeader,
	DialogTitle,
	DialogTrigger,
} from "@/components/ui/dialog";
import {
	Form,
	FormControl,
	FormField,
	FormItem,
	FormLabel,
	FormMessage,
} from "@/components/ui/form";
import { Input } from "@/components/ui/input";
import {
	Select,
	SelectContent,
	SelectItem,
	SelectTrigger,
	SelectValue,
} from "@/components/ui/select";
import { api } from "@/utils/api";

const schema = z.object({
	registryImage: z.string().min(1, "Registry image is required"),
	registryId: z.string().optional(),
	serviceName: z.string().min(1, "Service name is required"),
});

type Schema = z.infer<typeof schema>;

export const UpdateSourceConfig = () => {
	const [isOpen, setIsOpen] = useState(false);
	const [testResult, setTestResult] = useState<{
		success: boolean;
		latestVersion?: string | null;
		updateAvailable?: boolean;
	} | null>(null);

	const { data: config, refetch } =
		api.settings.getRegistryConfig.useQuery();
	const { data: registries } = api.registry.all.useQuery();

	const {
		mutateAsync: updateConfig,
		isPending: isSaving,
		error: saveError,
		isError: isSaveError,
	} = api.settings.updateRegistryConfig.useMutation();

	const {
		mutateAsync: testConnection,
		isPending: isTesting,
	} = api.settings.testRegistryConnection.useMutation();

	const form = useForm<Schema>({
		defaultValues: {
			registryImage: "",
			registryId: "",
			serviceName: "dokploy",
		},
		resolver: zodResolver(schema),
	});

	useEffect(() => {
		if (config) {
			form.reset({
				registryImage: config.registryImage || "",
				registryId: config.registryId || "",
				serviceName: config.serviceName || "dokploy",
			});
		}
	}, [config, form]);

	const onSubmit = async (data: Schema) => {
		await updateConfig({
			registryImage: data.registryImage,
			registryId: data.registryId || null,
			serviceName: data.serviceName,
		})
			.then(async () => {
				toast.success("Update source config saved");
				await refetch();
				setTestResult(null);
			})
			.catch(() => {
				toast.error("Error saving update source config");
			});
	};

	const handleTest = async () => {
		try {
			// Save first, then test
			const data = form.getValues();
			await updateConfig({
				registryImage: data.registryImage,
				registryId: data.registryId || null,
				serviceName: data.serviceName,
			});
			await refetch();

			const result = await testConnection();
			setTestResult(result);
			if (result.success) {
				if (result.updateAvailable && result.latestVersion) {
					toast.success(
						`Connected! Latest version: ${result.latestVersion}`,
					);
				} else {
					toast.success("Connected! You are up to date.");
				}
			}
		} catch (error) {
			setTestResult(null);
			toast.error("Connection test failed. Check your registry config.");
		}
	};

	return (
		<Dialog open={isOpen} onOpenChange={(open) => { setIsOpen(open); if (!open) setTestResult(null); }}>
			<DialogTrigger asChild>
				<Button variant="outline" size="sm">
					<Settings2 className="h-4 w-4" />
					Update Source
				</Button>
			</DialogTrigger>
			<DialogContent className="max-w-lg">
				<DialogHeader>
					<DialogTitle>Update Source Config</DialogTitle>
					<DialogDescription>
						Configure the registry and image for checking and applying updates.
						Select a registry from your existing registries for authentication.
					</DialogDescription>
				</DialogHeader>

				{isSaveError && (
					<AlertBlock type="error">{saveError?.message}</AlertBlock>
				)}

				{testResult && (
					<div className="flex items-center gap-2 rounded-lg px-3 py-2 border border-emerald-900 bg-emerald-900/40">
						<CheckCircle2 className="h-4 w-4 text-emerald-400 flex-shrink-0" />
						<span className="text-sm text-emerald-300">
							{testResult.updateAvailable && testResult.latestVersion
								? `Connected. Latest version: ${testResult.latestVersion}`
								: "Connected. You are up to date."}
						</span>
					</div>
				)}

				<Form {...form}>
					<form
						id="hook-form-update-source"
						onSubmit={form.handleSubmit(onSubmit)}
						className="space-y-4"
					>
						<FormField
							control={form.control}
							name="registryImage"
							render={({ field }) => (
								<FormItem>
									<FormLabel>Registry Image</FormLabel>
									<FormControl>
										<Input
											placeholder="e.g. crpi-xxx.cn-shanghai.personal.cr.aliyuncs.com/tigerking/dokploy-go"
											{...field}
										/>
									</FormControl>
									<FormMessage />
								</FormItem>
							)}
						/>

						<FormField
							control={form.control}
							name="registryId"
							render={({ field }) => (
								<FormItem>
									<FormLabel>Registry (Authentication)</FormLabel>
									<Select
										onValueChange={field.onChange}
										value={field.value}
									>
										<FormControl>
											<SelectTrigger>
												<SelectValue placeholder="Select a registry (optional)" />
											</SelectTrigger>
										</FormControl>
										<SelectContent>
											<SelectItem value="">
												None (Public Registry)
											</SelectItem>
											{registries?.map((r: any) => (
												<SelectItem
													key={r.registryId}
													value={r.registryId}
												>
													{r.registryName} ({r.registryUrl})
												</SelectItem>
											))}
										</SelectContent>
									</Select>
									<FormMessage />
								</FormItem>
							)}
						/>

						<FormField
							control={form.control}
							name="serviceName"
							render={({ field }) => (
								<FormItem>
									<FormLabel>Service Name</FormLabel>
									<FormControl>
										<Input
											placeholder="dokploy"
											{...field}
										/>
									</FormControl>
									<FormMessage />
								</FormItem>
							)}
						/>
					</form>
				</Form>

				<DialogFooter className="gap-2 sm:gap-0">
					<Button
						type="button"
						variant="outline"
						onClick={handleTest}
						disabled={isTesting || !form.getValues("registryImage")}
					>
						{isTesting ? (
							<Loader2 className="h-4 w-4 animate-spin" />
						) : (
							<Wifi className="h-4 w-4" />
						)}
						Test Connection
					</Button>
					<Button
						isLoading={isSaving}
						disabled={isSaving}
						form="hook-form-update-source"
						type="submit"
					>
						Save
					</Button>
				</DialogFooter>
			</DialogContent>
		</Dialog>
	);
};
