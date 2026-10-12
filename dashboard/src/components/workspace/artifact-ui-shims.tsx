// Small, local equivalents for common model-generated shadcn imports.
// They live only inside the artifact frame and have no app/API context.
import * as React from "react";

function join(base: string, extra?: string) { return `${base} ${extra ?? ""}`; }

export function Button({ variant, className, ...props }: React.ComponentProps<"button"> & { variant?: string }) {
  const color = variant === "outline" ? "border border-zinc-300 bg-white text-zinc-900" :
    variant === "ghost" ? "bg-transparent text-zinc-900" :
      variant === "destructive" ? "bg-red-600 text-white" : "bg-zinc-900 text-white";
  return <button className={join(`inline-flex items-center justify-center gap-2 rounded-md px-3 py-2 text-sm font-medium ${color}`, className)} {...props} />;
}

export function Card({ className, ...props }: React.ComponentProps<"div">) {
  return <div className={join("rounded-xl border border-zinc-200 bg-white shadow-sm", className)} {...props} />;
}
export function CardHeader({ className, ...props }: React.ComponentProps<"div">) {
  return <div className={join("p-5 pb-2", className)} {...props} />;
}
export function CardTitle({ className, ...props }: React.ComponentProps<"div">) {
  return <div className={join("text-lg font-semibold", className)} {...props} />;
}
export function CardDescription({ className, ...props }: React.ComponentProps<"div">) {
  return <div className={join("text-sm text-zinc-500", className)} {...props} />;
}
export function CardContent({ className, ...props }: React.ComponentProps<"div">) {
  return <div className={join("p-5 pt-2", className)} {...props} />;
}
export function CardFooter({ className, ...props }: React.ComponentProps<"div">) {
  return <div className={join("flex items-center gap-2 p-5 pt-2", className)} {...props} />;
}
export function CardAction({ className, ...props }: React.ComponentProps<"div">) {
  return <div className={join("ml-auto", className)} {...props} />;
}

export function Badge({ className, variant, ...props }: React.ComponentProps<"span"> & { variant?: string }) {
  return <span className={join(`inline-flex rounded-full px-2 py-0.5 text-xs font-medium ${variant === "outline" ? "border border-zinc-300" : "bg-zinc-100 text-zinc-800"}`, className)} {...props} />;
}
export function Input({ className, ...props }: React.ComponentProps<"input">) {
  return <input className={join("w-full rounded-md border border-zinc-300 bg-white px-3 py-2 text-sm", className)} {...props} />;
}
export function Textarea({ className, ...props }: React.ComponentProps<"textarea">) {
  return <textarea className={join("w-full rounded-md border border-zinc-300 bg-white px-3 py-2 text-sm", className)} {...props} />;
}
export function Separator({ className, ...props }: React.ComponentProps<"hr">) {
  return <hr className={join("my-2 border-zinc-200", className)} {...props} />;
}
export function Progress({ value = 0, className }: { value?: number; className?: string }) {
  return <div className={join("h-2 overflow-hidden rounded-full bg-zinc-200", className)}><div className="h-full bg-zinc-900" style={{ width: `${Math.max(0, Math.min(100, value))}%` }} /></div>;
}
export function Alert({ className, ...props }: React.ComponentProps<"div">) {
  return <div role="alert" className={join("rounded-md border border-zinc-300 p-4", className)} {...props} />;
}
export function AlertTitle({ className, ...props }: React.ComponentProps<"div">) {
  return <div className={join("font-semibold", className)} {...props} />;
}
export function AlertDescription({ className, ...props }: React.ComponentProps<"div">) {
  return <div className={join("text-sm text-zinc-600", className)} {...props} />;
}

type TabsState = { value: string; setValue: (value: string) => void };
const TabsContext = React.createContext<TabsState | null>(null);
export function Tabs({ value, defaultValue = "", onValueChange, className, children }: {
  value?: string; defaultValue?: string; onValueChange?: (value: string) => void;
  className?: string; children?: React.ReactNode;
}) {
  const [local, setLocal] = React.useState(defaultValue);
  const current = value ?? local;
  const setValue = (next: string) => { setLocal(next); onValueChange?.(next); };
  return <TabsContext.Provider value={{ value: current, setValue }}><div className={className}>{children}</div></TabsContext.Provider>;
}
export function TabsList({ className, ...props }: React.ComponentProps<"div">) {
  return <div role="tablist" className={join("inline-flex rounded-md bg-zinc-100 p-1", className)} {...props} />;
}
export function TabsTrigger({ value, className, onClick, ...props }: React.ComponentProps<"button"> & { value: string }) {
  const state = React.useContext(TabsContext);
  return <button role="tab" aria-selected={state?.value === value} onClick={(event) => { state?.setValue(value); onClick?.(event); }} className={join(`rounded px-3 py-1.5 text-sm ${state?.value === value ? "bg-white shadow-sm" : "text-zinc-600"}`, className)} {...props} />;
}
export function TabsContent({ value, className, ...props }: React.ComponentProps<"div"> & { value: string }) {
  const state = React.useContext(TabsContext);
  return state?.value === value ? <div role="tabpanel" className={join("mt-3", className)} {...props} /> : null;
}

export function Table({ className, ...props }: React.ComponentProps<"table">) { return <table className={join("w-full border-collapse text-sm", className)} {...props} />; }
export function TableHeader(props: React.ComponentProps<"thead">) { return <thead {...props} />; }
export function TableBody(props: React.ComponentProps<"tbody">) { return <tbody {...props} />; }
export function TableFooter(props: React.ComponentProps<"tfoot">) { return <tfoot {...props} />; }
export function TableRow({ className, ...props }: React.ComponentProps<"tr">) { return <tr className={join("border-b border-zinc-200", className)} {...props} />; }
export function TableHead({ className, ...props }: React.ComponentProps<"th">) { return <th className={join("px-3 py-2 text-left font-semibold", className)} {...props} />; }
export function TableCell({ className, ...props }: React.ComponentProps<"td">) { return <td className={join("px-3 py-2", className)} {...props} />; }
export function TableCaption({ className, ...props }: React.ComponentProps<"caption">) { return <caption className={join("py-2 text-zinc-500", className)} {...props} />; }
