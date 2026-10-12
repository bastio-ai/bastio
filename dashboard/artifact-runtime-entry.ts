// Bundled by vite-plugin-artifact-runtime.ts and injected into an opaque-origin
// iframe. This runtime has no workspace API client, credentials, or app state.
import * as React from "react";
import * as ReactDOM from "react-dom/client";
import * as ReactDOMLegacy from "react-dom";
import * as JSXRuntime from "react/jsx-runtime";
import * as Lucide from "lucide-react";
import * as Recharts from "recharts";
import * as Motion from "framer-motion";
import * as DateFns from "date-fns";
import * as UI from "./src/components/workspace/artifact-ui-shims";

Object.defineProperty(globalThis, "__BASTIO_ARTIFACT_MODULES__", {
  value: {
    react: React,
    "react/jsx-runtime": JSXRuntime,
    "react-dom/client": ReactDOM,
    "react-dom": ReactDOMLegacy,
    "lucide-react": Lucide,
    recharts: Recharts,
    "framer-motion": Motion,
    "date-fns": DateFns,
    "@/components/ui/button": { Button: UI.Button },
    "@/components/ui/card": {
      Card: UI.Card, CardHeader: UI.CardHeader, CardTitle: UI.CardTitle,
      CardDescription: UI.CardDescription, CardContent: UI.CardContent,
      CardFooter: UI.CardFooter, CardAction: UI.CardAction,
    },
    "@/components/ui/badge": { Badge: UI.Badge },
    "@/components/ui/input": { Input: UI.Input },
    "@/components/ui/textarea": { Textarea: UI.Textarea },
    "@/components/ui/separator": { Separator: UI.Separator },
    "@/components/ui/progress": { Progress: UI.Progress },
    "@/components/ui/alert": {
      Alert: UI.Alert, AlertTitle: UI.AlertTitle, AlertDescription: UI.AlertDescription,
    },
    "@/components/ui/tabs": {
      Tabs: UI.Tabs, TabsList: UI.TabsList,
      TabsTrigger: UI.TabsTrigger, TabsContent: UI.TabsContent,
    },
    "@/components/ui/table": {
      Table: UI.Table, TableHeader: UI.TableHeader, TableBody: UI.TableBody,
      TableFooter: UI.TableFooter, TableRow: UI.TableRow,
      TableHead: UI.TableHead, TableCell: UI.TableCell,
      TableCaption: UI.TableCaption,
    },
  },
  configurable: false,
  writable: false,
});
