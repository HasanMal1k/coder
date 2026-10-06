"use client";

import { EyeIcon, EyeOffIcon, TriangleAlertIcon } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useRef, useState, type FormEvent } from "react";
import { errorMessage, login, register } from "@/lib/api";
import { dismissNotice, peekNotice, setSession, takeReturnTo, useSession } from "@/lib/session";
import { Alert, AlertDescription } from "../ui/alert";
import { Button } from "../ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "../ui/card";
import { Field, FieldDescription, FieldError, FieldGroup, FieldLabel } from "../ui/field";
import { Input } from "../ui/input";
import { InputGroup, InputGroupAddon, InputGroupButton, InputGroupInput } from "../ui/input-group";
import { Spinner } from "../ui/spinner";

const MIN_PASSWORD_LENGTH = 8;
// bcrypt, which the server uses, ignores everything past 72 bytes.
const MAX_PASSWORD_LENGTH = 72;

type Problems = { name?: string; email?: string; password?: string };

export function AuthForm({ mode }: { mode: "login" | "register" }) {
  const isLogin = mode === "login";
  const router = useRouter();
  const session = useSession();

  const [name, setName] = useState("");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [showPassword, setShowPassword] = useState(false);
  const [problems, setProblems] = useState<Problems>({});
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [notice] = useState(peekNotice);

  // The notice belongs to this visit to the page only.
  useEffect(() => dismissNotice, []);

  // Signed in (already, or just now): go where they were headed.
  const redirected = useRef(false);
  useEffect(() => {
    if (session && !redirected.current) {
      redirected.current = true;
      router.replace(takeReturnTo());
    }
  }, [session, router]);

  function validate(): Problems {
    const found: Problems = {};

    if (!isLogin && name.trim() === "") found.name = "Enter your name.";
    if (!/^\S+@\S+\.\S+$/.test(email.trim())) found.email = "Enter a valid email address.";
    if (password === "") {
      found.password = "Enter your password.";
    } else if (!isLogin && password.length < MIN_PASSWORD_LENGTH) {
      found.password = `Use at least ${MIN_PASSWORD_LENGTH} characters.`;
    }

    return found;
  }

  async function onSubmit(event: FormEvent) {
    event.preventDefault();

    const found = validate();
    setProblems(found);
    if (Object.keys(found).length > 0) return;

    setError(null);
    setSubmitting(true);
    try {
      const result = isLogin
        ? await login(email.trim(), password)
        : await register({
            email: email.trim(),
            password,
            display_name: name.trim(),
            organization: "",
          });
      setSession(result);
    } catch (err) {
      setError(errorMessage(err));
      setSubmitting(false);
    }
  }

  if (session !== null) {
    return <Spinner className="size-5 text-muted-foreground" role="status" aria-label="Loading" />;
  }

  return (
    <div className="flex w-full max-w-sm flex-col gap-5">
      <Card>
        <CardHeader>
          <CardTitle className="text-xl">
            {isLogin ? "Welcome back" : "Create your account"}
          </CardTitle>
          <CardDescription>
            {isLogin
              ? "Sign in. Your machines are where you left them."
              : "A Linux machine with VS Code and a terminal, in your browser."}
          </CardDescription>
        </CardHeader>
        <CardContent>
          <form onSubmit={onSubmit} noValidate>
            <FieldGroup>
              {notice && !error && (
                <Alert>
                  <TriangleAlertIcon className="text-warning" />
                  <AlertDescription>{notice}</AlertDescription>
                </Alert>
              )}
              {error && (
                <Alert variant="destructive">
                  <TriangleAlertIcon />
                  <AlertDescription>{error}</AlertDescription>
                </Alert>
              )}

              {!isLogin && (
                <Field data-invalid={problems.name ? true : undefined}>
                  <FieldLabel htmlFor="name">Name</FieldLabel>
                  <Input
                    id="name"
                    name="display_name"
                    autoComplete="name"
                    maxLength={80}
                    value={name}
                    aria-invalid={problems.name ? true : undefined}
                    onChange={(e) => setName(e.target.value)}
                  />
                  {problems.name && <FieldError>{problems.name}</FieldError>}
                </Field>
              )}

              <Field data-invalid={problems.email ? true : undefined}>
                <FieldLabel htmlFor="email">Email</FieldLabel>
                <Input
                  id="email"
                  name="email"
                  type="email"
                  autoComplete="email"
                  inputMode="email"
                  spellCheck={false}
                  placeholder="you@example.com"
                  value={email}
                  aria-invalid={problems.email ? true : undefined}
                  onChange={(e) => setEmail(e.target.value)}
                />
                {problems.email && <FieldError>{problems.email}</FieldError>}
              </Field>

              <Field data-invalid={problems.password ? true : undefined}>
                <FieldLabel htmlFor="password">Password</FieldLabel>
                <InputGroup>
                  <InputGroupInput
                    id="password"
                    name="password"
                    type={showPassword ? "text" : "password"}
                    autoComplete={isLogin ? "current-password" : "new-password"}
                    maxLength={MAX_PASSWORD_LENGTH}
                    value={password}
                    aria-invalid={problems.password ? true : undefined}
                    onChange={(e) => setPassword(e.target.value)}
                  />
                  <InputGroupAddon align="inline-end">
                    <InputGroupButton
                      size="icon-xs"
                      aria-label={showPassword ? "Hide password" : "Show password"}
                      onClick={() => setShowPassword((s) => !s)}
                    >
                      {showPassword ? <EyeOffIcon /> : <EyeIcon />}
                    </InputGroupButton>
                  </InputGroupAddon>
                </InputGroup>
                {problems.password ? (
                  <FieldError>{problems.password}</FieldError>
                ) : (
                  !isLogin && (
                    <FieldDescription>At least {MIN_PASSWORD_LENGTH} characters.</FieldDescription>
                  )
                )}
              </Field>

              <Button type="submit" size="lg" disabled={submitting}>
                {submitting && <Spinner />}
                {isLogin ? "Sign in" : "Create account"}
              </Button>
            </FieldGroup>
          </form>
        </CardContent>
      </Card>

      <p className="text-center text-sm text-muted-foreground">
        {isLogin ? "New here? " : "Already have an account? "}
        <Link
          href={isLogin ? "/register" : "/login"}
          className="font-medium text-foreground underline decoration-primary/60 underline-offset-4 hover:decoration-primary"
        >
          {isLogin ? "Create an account" : "Sign in"}
        </Link>
      </p>
    </div>
  );
}
