import { z } from "zod";

export const uploadFileSchema = z.object({
	applicationId: z.string().optional(),
	zip: z.any(),
	dropBuildPath: z.string().optional(),
});

export type UploadFile = z.infer<typeof uploadFileSchema>;
