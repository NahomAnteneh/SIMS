"use client";

import React, { useState } from "react";
import { useRouter } from "next/navigation";
import { cn } from "@/lib/utils";

import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import {
  Field,
  FieldDescription,
  FieldGroup,
  FieldLabel,
} from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { simsApi } from "@/lib/api/client";
import { Warehouse, Loader2, AlertCircle } from "lucide-react";

export function LoginForm({
  className,
  ...props
}: React.ComponentProps<"div">) {
  const router = useRouter();
  const [username, setUsername] = useState("admin");
  const [password, setPassword] = useState("admin123");
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);
    setLoading(true);

    try {
      await simsApi.login({ username, password });
      router.push("/dashboard");
      router.refresh();
    } catch (err: any) {
      setError(
        err?.message || "Invalid credentials. Please verify username and password."
      );
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className={cn("flex flex-col gap-6", className)} {...props}>
      <Card className="border-zinc-800 bg-[#0c0c0c] text-zinc-100 shadow-2xl">
        <CardHeader className="text-center">
          <div className="mx-auto mb-2 flex h-10 w-10 items-center justify-center rounded-lg bg-yellow-600 text-black shadow-lg">
            <Warehouse className="h-5 w-5" />
          </div>
          <CardTitle className="text-xl font-bold tracking-tight text-zinc-100">
            Sign in to SIMS Depot
          </CardTitle>
          <CardDescription className="text-zinc-400">
            Distributed vSphere Inventory & Search Platform
          </CardDescription>
        </CardHeader>
        <CardContent>
          <form onSubmit={handleSubmit}>
            <FieldGroup>
              {error && (
                <div className="flex items-center gap-2 rounded-lg border border-rose-500/30 bg-rose-500/10 p-3 text-xs text-rose-400">
                  <AlertCircle className="h-4 w-4 shrink-0" />
                  <span>{error}</span>
                </div>
              )}

              <Field>
                <FieldLabel htmlFor="username">Username</FieldLabel>
                <Input
                  id="username"
                  type="text"
                  placeholder="admin"
                  value={username}
                  onChange={(e) => setUsername(e.target.value)}
                  required
                  className="bg-zinc-900 border-zinc-800 text-zinc-100"
                />
              </Field>

              <Field>
                <FieldLabel htmlFor="password">Password</FieldLabel>
                <Input
                  id="password"
                  type="password"
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  required
                  className="bg-zinc-900 border-zinc-800 text-zinc-100"
                />
              </Field>

              <Field className="pt-2">
                <Button
                  type="submit"
                  disabled={loading}
                  className="w-full bg-yellow-600 hover:bg-yellow-500 text-black font-semibold"
                >
                  {loading ? (
                    <>
                      <Loader2 className="mr-2 h-4 w-4 animate-spin" />
                      Authenticating...
                    </>
                  ) : (
                    "Sign In"
                  )}
                </Button>
                <FieldDescription className="text-center text-zinc-500 text-xs mt-3">
                  Default bootstrap credentials: <span className="text-zinc-400 font-mono">admin</span> / <span className="text-zinc-400 font-mono">admin123</span>
                </FieldDescription>
              </Field>
            </FieldGroup>
          </form>
        </CardContent>
      </Card>
    </div>
  );
}
