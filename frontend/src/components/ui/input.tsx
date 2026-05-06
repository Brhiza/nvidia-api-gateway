import * as React from "react"
import { Input as InputPrimitive } from "@base-ui/react/input"

import { cn } from "@/lib/utils"

function Input({ className, type, ...props }: React.ComponentProps<"input">) {
  return (
    <InputPrimitive
      type={type}
      data-slot="input"
      className={cn(
        "h-10 w-full min-w-0 rounded-2xl border border-slate-200 bg-white px-4 py-2 text-sm text-slate-800 shadow-sm outline-none placeholder:text-slate-400 focus-visible:border-sky-300 focus-visible:ring-4 focus-visible:ring-sky-100 disabled:pointer-events-none disabled:cursor-not-allowed disabled:bg-slate-100 disabled:text-slate-400",
        className,
      )}
      {...props}
    />
  )
}

export { Input }
