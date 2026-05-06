"use client";

import * as React from "react";

import { cn } from "@/lib/utils";

function Switch({ checked, onCheckedChange, className }: { checked: boolean; onCheckedChange: (checked: boolean) => void; className?: string }) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      onClick={() => onCheckedChange(!checked)}
      className={cn(
        "relative inline-flex h-7 w-12 items-center rounded-full border transition-colors focus-visible:outline-none focus-visible:ring-4 focus-visible:ring-sky-100",
        checked ? "border-sky-300 bg-sky-500/90" : "border-slate-200 bg-slate-200",
        className,
      )}
    >
      <span
        className={cn(
          "inline-block h-5 w-5 transform rounded-full bg-white shadow-sm transition-transform",
          checked ? "translate-x-6" : "translate-x-1",
        )}
      />
    </button>
  );
}

export { Switch };
