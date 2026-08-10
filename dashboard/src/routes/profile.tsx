import { useState } from "react";
import { Shield, Check, Smartphone, History } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Card } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";

export function ProfilePage() {
  const [name, setName] = useState("Developer User");
  const [email, setEmail] = useState("developer@bastio.ai");
  const [saved, setSaved] = useState(false);

  const handleSave = (e: React.FormEvent) => {
    e.preventDefault();
    setSaved(true);
    setTimeout(() => setSaved(false), 3000);
  };

  return (
    <div className="space-y-6 max-w-4xl">
      <div>
        <h1 className="text-2xl font-bold tracking-tight text-foreground">Account Profile & Security</h1>
        <p className="text-sm text-muted-foreground mt-1">
          Manage your personal account credentials, security preferences, and connected providers.
        </p>
      </div>

      {/* Personal Info Card */}
      <Card className="p-6 space-y-4">
        <div className="flex items-center gap-3 pb-4 border-b border-border-subtle">
          <div className="h-10 w-10 rounded-full bg-gradient-to-tr from-amber-500 to-amber-300 text-black font-bold text-sm flex items-center justify-center">
            {name.slice(0, 2).toUpperCase()}
          </div>
          <div>
            <h2 className="text-base font-semibold text-foreground">{name}</h2>
            <p className="text-xs text-muted-foreground">{email}</p>
          </div>
        </div>

        <form onSubmit={handleSave} className="space-y-4 pt-2">
          <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
            <div className="space-y-1.5">
              <label className="text-xs font-medium text-foreground">Full Name</label>
              <Input
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="Your Name"
              />
            </div>
            <div className="space-y-1.5">
              <label className="text-xs font-medium text-foreground">Email Address</label>
              <Input
                value={email}
                onChange={(e) => setEmail(e.target.value)}
                placeholder="name@company.com"
              />
            </div>
          </div>

          <div className="flex items-center justify-between pt-2">
            {saved && (
              <span className="text-xs text-emerald-500 flex items-center gap-1 font-medium">
                <Check className="h-3.5 w-3.5" /> Profile changes saved successfully
              </span>
            )}
            <Button type="submit" size="sm" className="ml-auto">
              Save Profile
            </Button>
          </div>
        </form>
      </Card>

      {/* Security & Authentication */}
      <Card className="p-6 space-y-4">
        <div className="flex items-center justify-between pb-3 border-b border-border-subtle">
          <div className="flex items-center gap-2">
            <Shield className="h-4 w-4 text-amber-500" />
            <h2 className="text-base font-semibold text-foreground">Authentication & 2FA</h2>
          </div>
          <Badge variant="outline" className="text-emerald-500 border-emerald-500/30">
            Protected
          </Badge>
        </div>

        <div className="space-y-4">
          <div className="flex items-center justify-between py-2 border-b border-border-subtle/50">
            <div>
              <p className="text-sm font-medium text-foreground">Two-Factor Authentication (2FA)</p>
              <p className="text-xs text-muted-foreground">Add an extra layer of security using an authenticator app (TOTP).</p>
            </div>
            <Button variant="outline" size="sm">
              <Smartphone className="h-3.5 w-3.5 mr-1.5" /> Enable 2FA
            </Button>
          </div>

          <div className="flex items-center justify-between py-2 border-b border-border-subtle/50">
            <div>
              <p className="text-sm font-medium text-foreground">Connected Identity Providers</p>
              <p className="text-xs text-muted-foreground">Signed in via GitHub OAuth (developer@bastio.ai)</p>
            </div>
            <Badge variant="secondary" className="font-mono text-xs">
              GitHub OAuth
            </Badge>
          </div>

          <div className="flex items-center justify-between py-2">
            <div>
              <p className="text-sm font-medium text-foreground">Active Web Sessions</p>
              <p className="text-xs text-muted-foreground">1 active browser session in Frankfurt, DE</p>
            </div>
            <Button variant="ghost" size="sm" className="text-red-500 hover:text-red-600 hover:bg-red-500/10">
              <History className="h-3.5 w-3.5 mr-1.5" /> Sign out all devices
            </Button>
          </div>
        </div>
      </Card>
    </div>
  );
}
