import type { Metadata } from "next";
import { AuthForm } from "@/components/auth/auth-form";

export const metadata: Metadata = { title: "Sign in · coder" };

export default function LoginPage() {
  return <AuthForm mode="login" />;
}
