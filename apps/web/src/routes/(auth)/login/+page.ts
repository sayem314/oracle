import { redirect } from "@sveltejs/kit";
import { getSession } from "$lib/api";

export async function load(): Promise<Record<string, never>> {
  const user = await getSession();
  if (user) {
    throw redirect(307, "/");
  }
  return {};
}
