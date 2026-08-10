import { useEffect, useState } from "react";
import { useNavigate } from "@tanstack/react-router";
import { Zap, Shield, Home } from "lucide-react";
import { Button } from "@/components/ui/button";

interface CloudParticle {
  id: number;
  x: number;
  y: number;
  size: number;
  speedX: number;
  speedY: number;
  opacity: number;
  scale: number;
  rotation: number;
  rotationSpeed: number;
  label: string;
}

const KNOWLEDGE_SNIPPETS = [
  "HTTP 404: ROUTE_NOT_FOUND",
  "bastio://gateway/null",
  "0xDEADBEEF :: PAGE_MISSING",
  "PII_REDACTED :: [UNKNOWN_PATH]",
  "ZERO_G_ENGINE :: FLOAT_MODE",
  "VECTOR_DB_MISS :: 0.00 SIMILARITY",
];

export function AntigravityCloud404() {
  const title = "404 — Route Not Found";
  const navigate = useNavigate();
  const [clouds, setClouds] = useState<CloudParticle[]>([]);
  const [terminalLog, setTerminalLog] = useState<string[]>([
    "INITIALIZING BASTIO QUANTUM ROUTER...",
    "SCANNING ENDPOINTS [--------------------] 0%",
    "PATH NOT FOUND: 404 NULL POINTER EXCEPTION",
    "ZERO-G SWITCH DETECTED: QUANTUM ENGINES ONLINE ☁️",
  ]);
  const [activeSnippetIdx, setActiveSnippetIdx] = useState(0);

  // Dynamic cloud particles floating in zero-gravity
  useEffect(() => {
    const initialClouds: CloudParticle[] = Array.from({ length: 14 }).map((_, i) => ({
      id: i,
      x: Math.random() * 100,
      y: Math.random() * 100,
      size: 40 + Math.random() * 80,
      speedX: (Math.random() - 0.5) * 0.06,
      speedY: -0.04 - Math.random() * 0.08,
      opacity: 0.15 + Math.random() * 0.35,
      scale: 0.8 + Math.random() * 0.4,
      rotation: Math.random() * 360,
      rotationSpeed: (Math.random() - 0.5) * 0.15,
      label: KNOWLEDGE_SNIPPETS[i % KNOWLEDGE_SNIPPETS.length] ?? "404",
    }));

    setClouds(initialClouds);

    const interval = setInterval(() => {
      setClouds((prev) =>
        prev.map((cloud) => {
          let nextY = cloud.y + cloud.speedY;
          let nextX = cloud.x + cloud.speedX;
          if (nextY < -15) nextY = 115;
          if (nextX < -15) nextX = 115;
          if (nextX > 115) nextX = -15;

          return {
            ...cloud,
            x: nextX,
            y: nextY,
            rotation: cloud.rotation + cloud.rotationSpeed,
          };
        })
      );
    }, 40);

    return () => clearInterval(interval);
  }, []);

  // Terminal simulated typing
  useEffect(() => {
    const interval = setInterval(() => {
      setActiveSnippetIdx((prev) => (prev + 1) % KNOWLEDGE_SNIPPETS.length);
    }, 3200);
    return () => clearInterval(interval);
  }, []);

  const handleCloudClick = (id: number) => {
    setClouds((prev) =>
      prev.map((c) =>
        c.id === id
          ? {
              ...c,
              speedY: -0.8 - Math.random() * 0.4,
              opacity: 0.95,
            }
          : c
      )
    );
    setTerminalLog((prev) => [
      ...prev.slice(-4),
      `ZERO-G THRUST APPLIED TO CLOUD #${id} 🚀`,
    ]);
  };

  return (
    <div className="relative min-h-[85vh] w-full overflow-hidden flex flex-col items-center justify-center bg-background text-foreground px-4 py-12 select-none transition-colors duration-300 font-sans">
      {/* Subtle Background Grid */}
      <div className="absolute inset-0 bg-[radial-gradient(var(--border)_1px,transparent_1px)] [background-size:24px_24px] opacity-30 dark:opacity-20 pointer-events-none" />
      <div className="absolute inset-0 bg-gradient-to-b from-primary/5 via-transparent to-background pointer-events-none" />

      {/* Floating Interactive Cloud Elements */}
      <div className="absolute inset-0 overflow-hidden pointer-events-auto">
        {clouds.map((cloud) => (
          <div
            key={cloud.id}
            onClick={() => handleCloudClick(cloud.id)}
            style={{
              left: `${cloud.x}%`,
              top: `${cloud.y}%`,
              opacity: cloud.opacity,
              transform: `scale(${cloud.scale}) rotate(${cloud.rotation}deg)`,
              transition: "transform 0.2s ease-out, opacity 0.3s ease",
            }}
            className="absolute cursor-pointer group flex flex-col items-center justify-center p-3 rounded-2xl backdrop-blur-md bg-card/60 border border-border/80 shadow-sm hover:border-primary/50 hover:bg-primary/5 transition-all duration-200"
          >
            <svg
              className="w-10 h-10 text-primary/70 group-hover:text-primary transition-colors filter drop-shadow-sm"
              fill="currentColor"
              viewBox="0 0 24 24"
            >
              <path d="M19.35 10.04C18.67 6.59 15.64 4 12 4 9.11 4 6.6 5.64 5.35 8.04 2.34 8.36 0 10.91 0 14c0 3.31 2.69 6 6 6h13c2.76 0 5-2.24 5-5 0-2.64-2.05-4.78-4.65-4.96z" />
            </svg>
            <span className="mt-1 font-mono text-[9px] font-medium tracking-wider text-muted-foreground group-hover:text-foreground uppercase">
              {cloud.label}
            </span>
          </div>
        ))}
      </div>

      {/* Clean Platform Hero Card */}
      <div className="relative z-10 max-w-xl w-full flex flex-col items-center text-center space-y-6 bg-card/90 dark:bg-card/80 backdrop-blur-xl p-8 sm:p-10 rounded-2xl border border-border shadow-xl transition-all">
        {/* Minimal System Badge */}
        <div className="inline-flex items-center gap-2 px-3 py-1 rounded-full border border-border bg-muted/50 text-muted-foreground font-mono text-[11px] font-medium tracking-wide">
          <Zap className="h-3.5 w-3.5 text-primary" />
          <span>BASTIO QUANTUM // 404 EXCEPTION</span>
        </div>

        {/* Clean, Refined Mono Typography 404 */}
        <div className="flex flex-col items-center justify-center">
          <span className="font-mono text-6xl sm:text-7xl font-light tracking-tighter text-foreground border-b border-border/60 pb-1">
            404
          </span>
        </div>

        <div className="space-y-2">
          <h2 className="text-xl sm:text-2xl font-semibold tracking-tight text-foreground">
            {title}
          </h2>
          <p className="text-sm text-muted-foreground max-w-md mx-auto leading-relaxed">
            The requested path does not exist on this gateway node. You can click any floating cloud in the background to apply zero-G thrust or return to the main dashboard.
          </p>
        </div>

        {/* Crisp Developer Terminal Widget */}
        <div className="w-full text-left rounded-lg bg-zinc-950 dark:bg-zinc-950 text-zinc-100 border border-zinc-800 p-4 font-mono text-[12px] space-y-1.5 shadow-inner">
          <div className="flex items-center justify-between text-[10px] text-zinc-400 border-b border-zinc-800 pb-2 mb-2">
            <div className="flex items-center gap-1.5">
              <span className="w-2.5 h-2.5 rounded-full bg-red-500/80 inline-block" />
              <span className="w-2.5 h-2.5 rounded-full bg-amber-500/80 inline-block" />
              <span className="w-2.5 h-2.5 rounded-full bg-emerald-500/80 inline-block" />
              <span className="ml-2 font-medium text-zinc-300">bastio-cli v2.4.0</span>
            </div>
            <span className="text-sky-400 font-mono text-[10px]">STATUS: 404_NOT_FOUND</span>
          </div>

          {terminalLog.map((log, idx) => (
            <div key={idx} className="leading-relaxed flex items-center gap-2">
              <span className="text-zinc-600 select-none">&gt;</span>
              <span className={idx === terminalLog.length - 1 ? "text-cyan-300 font-medium" : "text-emerald-400/90"}>
                {log}
              </span>
            </div>
          ))}

          <div className="flex items-center gap-2 pt-1">
            <span className="text-sky-400 font-bold">&gt;</span>
            <span className="text-zinc-100 animate-pulse font-bold">_</span>
            <span className="text-zinc-500 text-[10px]">
              {KNOWLEDGE_SNIPPETS[activeSnippetIdx]}
            </span>
          </div>
        </div>

        {/* Clean Action Buttons */}
        <div className="flex flex-wrap items-center justify-center gap-3 pt-2 w-full">
          <Button
            onClick={() => navigate({ to: "/" })}
            className="gap-2 font-medium px-5"
          >
            <Home className="h-4 w-4" />
            Return to Dashboard
          </Button>

          <Button
            variant="outline"
            onClick={() => navigate({ to: "/workspace" })}
            className="gap-2 font-medium px-5"
          >
            <Shield className="h-4 w-4 text-emerald-500" />
            Private AI Portal
          </Button>
        </div>
      </div>
    </div>
  );
}
