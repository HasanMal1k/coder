"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useState, type FormEvent } from "react";
import { errorMessage, login, register } from "@/lib/api";
import { dismissNotice, peekNotice, setSession, useSession } from "@/lib/session";
import { Button, ErrorNote, Field, PageSpinner, Spinner } from "./ui";

const MIN_PASSWORD_LENGTH = 8;

export function AuthForm({ mode }: { mode: "login" | "register" }) {
  const router = useRouter();
  const session = useSession();

  const [displayName, setDisplayName] = useState("");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [notice] = useState(peekNotice);

  // The notice belongs to this visit to the page only.
  useEffect(() => dismissNotice, []);

  // Already signed in (or just became so): there is nothing to do here.
  useEffect(() => {
    if (session) router.replace("/");
  }, [session, router]);

  async function onSubmit(event: FormEvent) {
    event.preventDefault();
    setError(null);
    setSubmitting(true);

    try {
      const result =
        mode === "login"
          ? await login(email, password)
          : await register({ email, password, display_name: displayName.trim(), organization: "" });
      setSession(result);
    } catch (err) {
      setError(errorMessage(err));
      setSubmitting(false);
    }
  }

  if (session !== null) return <PageSpinner />;

  const isLogin = mode === "login";

  return (
    <main className="flex flex-1 items-center justify-center px-4 py-12">
      <div className="w-full max-w-sm">
        <p className="mb-8 text-center font-mono text-lg font-semibold tracking-tight">
          <span className="text-accent">▍</span>coder
        </p>

        <form
          onSubmit={onSubmit}
          className="flex flex-col gap-4 rounded-xl border border-line bg-surface p-6"
        >
          <h1 className="text-lg font-semibold">
            {isLogin ? "Sign in" : "Create your account"}
          </h1>

          {notice && !error && (
            <p className="rounded-md border border-warn/40 bg-warn/10 px-3 py-2 text-sm text-warn">
              {notice}
            </p>
          )}
          {error && <ErrorNote>{error}</ErrorNote>}

          {!isLogin && (
            <Field
              label="Name"
              name="display_name"
              autoComplete="name"
              required
              maxLength={80}
              value={displayName}
              onChange={(e) => setDisplayName(e.target.value)}
            />
          )}
          <Field
            label="Email"
            name="email"
            type="email"
            autoComplete="email"
            required
            value={email}
            onChange={(e) => setEmail(e.target.value)}
          />
          <Field
            label="Password"
            name="password"
            type="password"
            autoComplete={isLogin ? "current-password" : "new-password"}
            required
            minLength={isLogin ? undefined : MIN_PASSWORD_LENGTH}
            maxLength={72}
            hint={isLogin ? undefined : `At least ${MIN_PASSWORD_LENGTH} characters.`}
            value={password}
            onChange={(e) => setPassword(e.target.value)}
          />

          <Button type="submit" variant="primary" className="mt-2 h-10" disabled={submitting}>
            {submitting && <Spinner />}
            {isLogin ? "Sign in" : "Create account"}
          </Button>
        </form>

        <p className="mt-6 text-center text-sm text-muted">
          {isLogin ? "New here? " : "Already have an account? "}
          <Link
            href={isLogin ? "/register" : "/login"}
            className="text-fg underline decoration-line-strong underline-offset-4 hover:decoration-accent"
          >
            {isLogin ? "Create an account" : "Sign in"}
          </Link>
        </p>
      </div>
    </main>
  );
}
