"use client"

import {
  CircleCheckIcon,
  InfoIcon,
  Loader2Icon,
  OctagonXIcon,
  TriangleAlertIcon,
} from "lucide-react"
import { useTheme } from "next-themes"
import { Toaster as Sonner, type ToasterProps } from "sonner"

const Toaster = ({ ...props }: ToasterProps) => {
  const { theme = "system" } = useTheme()

  return (
    <Sonner
      theme={theme as ToasterProps["theme"]}
      className="toaster group"
      icons={{
        success: <CircleCheckIcon className="size-4" />,
        info: <InfoIcon className="size-4" />,
        warning: <TriangleAlertIcon className="size-4" />,
        error: <OctagonXIcon className="size-4" />,
        loading: <Loader2Icon className="size-4 animate-spin" />,
      }}
      // sonner's own injected stylesheet hardcodes a system sans font stack
      // on the toaster root (not a CSS var), so only an inline style on that
      // same element — which always wins over any stylesheet rule regardless
      // of selector specificity — can put the app font back in.
      style={
        {
          "--normal-bg": "var(--popover)",
          "--normal-text": "var(--popover-foreground)",
          "--normal-border": "var(--border)",
          "--border-radius": "var(--radius)",
          fontFamily: "var(--font-sans)",
        } as React.CSSProperties
      }
      toastOptions={{
        // Per-type accents layer on top of the shared night-navy skin by
        // overriding the --normal-* custom properties the injected stylesheet
        // already reads border/text color from, instead of adding Tailwind
        // color utilities directly (those would lose to sonner's own
        // higher-specificity, unlayered `border`/`color` declarations).
        classNames: {
          success: "[--normal-border:var(--primary)] [--normal-text:var(--primary)]",
          error: "[--normal-border:var(--destructive)] [--normal-text:var(--destructive)]",
        },
      }}
      {...props}
    />
  )
}

export { Toaster }
