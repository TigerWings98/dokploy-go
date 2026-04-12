import type { NextPage } from "next";
import dynamic from "next/dynamic";
import { useRouter } from "next/router";
import { api } from "@/utils/api";
import "swagger-ui-react/swagger-ui.css";
import { useEffect, useState } from "react";

const SwaggerUI = dynamic(() => import("swagger-ui-react"), { ssr: false });

const Home: NextPage = () => {
	const router = useRouter();
	// 与 TS v0.28.7 对齐：使用 getPermissions 代替 canAccessToAPI
	const { data: permissions, isPending } = api.user.getPermissions.useQuery();
	const { data } = api.settings.getOpenApiDocument.useQuery();
	const [spec, setSpec] = useState({});

	useEffect(() => {
		if (isPending) return;
		if (!permissions?.api.read) {
			router.replace("/");
		}
	}, [permissions, isPending, router]);

	useEffect(() => {
		if (data) {
			const protocolAndHost = `${window.location.protocol}//${window.location.host}/api`;
			// Force OpenAPI 3.0 so Swagger UI uses the 3.0 parser (avoids ApiDOM 3.1 refract bug)
			const newSpec = {
				...data,
				openapi: "3.0.3",
				servers: [{ url: protocolAndHost }],
				externalDocs: {
					url: `${protocolAndHost}/trpc/settings.getOpenApiDocument`,
				},
			};
			// Remove 3.1-only fields that could confuse the 3.0 parser
			if ("jsonSchemaDialect" in newSpec) {
				delete (newSpec as Record<string, unknown>).jsonSchemaDialect;
			}
			setSpec(newSpec);
		}
	}, [data]);

	if (isPending || !permissions?.api.read) return null;

	return (
		<div className="h-screen bg-white">
			<SwaggerUI
				spec={spec}
				persistAuthorization={true}
				plugins={[
					{
						statePlugins: {
							auth: {
								wrapActions: {
									authorize: (ori: any) => (args: any) => {
										const result = ori(args);
										const apiKey = args?.apiKey?.value;
										if (apiKey) {
											localStorage.setItem("swagger_api_key", apiKey);
										}
										return result;
									},
									logout: (ori: any) => (args: any) => {
										const result = ori(args);
										localStorage.removeItem("swagger_api_key");
										return result;
									},
								},
							},
						},
					},
				]}
				requestInterceptor={(request: any) => {
					const apiKey = localStorage.getItem("swagger_api_key");
					if (apiKey) {
						request.headers = request.headers || {};
						request.headers["x-api-key"] = apiKey;
					}
					return request;
				}}
			/>
		</div>
	);
};

export default Home;
