import { redirect } from "@sveltejs/kit";
import { getSession, type User } from "$lib/api";

export async function load(): Promise<{ user: User }> {
  const user = await getSession();
  if (!user) {
    throw redirect(307, "/login");
  }
  return { user };
}
