import { 
  LayoutGrid, Activity, Bell, Package, Layers, Grid, 
  ShoppingCart, Receipt, SlidersHorizontal, ArrowLeftRight, RotateCcw,
  Users, Warehouse, BarChart3, TrendingUp, RefreshCcw, 
  PieChart, Settings, Users2, Puzzle, ChevronDown
} from 'lucide-react';
import Link from 'next/link';

const menuGroups = [
  {
    title: "OVERVIEW",
    items: [
      { name: "Dashboard", icon: LayoutGrid, active: false },
      { name: "Activity", icon: Activity, active: false },
      { name: "Notifications", icon: Bell, active: false, badge: 6 },
    ]
  },
  {
    title: "INVENTORY",
    items: [
      { name: "Products", icon: Package, active: false },
      { name: "Categories", icon: Layers, active: false },
      { name: "Stock Matrix", icon: Grid, active: false },
    ]
  },
  {
    title: "OPERATIONS",
    items: [
      { name: "Sales Orders", icon: ShoppingCart, active: false },
      { name: "Purchase Orders", icon: Receipt, active: false },
      { name: "Adjustments", icon: SlidersHorizontal, active: false },
      { name: "Transfers", icon: ArrowLeftRight, active: false },
      { name: "Cycle Counts", icon: RotateCcw, active: false },
    ]
  },
  {
    title: "PARTNERS",
    items: [
      { name: "Suppliers", icon: Users, active: false },
      { name: "Warehouses", icon: Warehouse, active: false },
    ]
  },
  {
    title: "ANALYTICS",
    items: [
      { name: "Reports", icon: BarChart3, active: true },
      { name: "Stock Value", icon: TrendingUp, active: false },
      { name: "Turnover", icon: RefreshCcw, active: false },
      { name: "Suppliers", icon: Users2, active: false },
      { name: "ABC Analysis", icon: PieChart, active: false },
    ]
  },
  {
    title: "SYSTEM",
    items: [
      { name: "Settings", icon: Settings, active: false },
      { name: "Team", icon: Users2, active: false },
      { name: "Integrations", icon: Puzzle, active: false },
    ]
  }
];

export const Sidebar = () => {
  return (
    <aside className="w-64 border-r border-zinc-900 bg-[#0a0a0a] flex flex-col h-full">
      {/* Logo */}
      <div className="h-14 flex items-center px-4 gap-3">
        <div className="h-6 w-6 rounded bg-yellow-600 flex items-center justify-center">
          <Warehouse className="h-4 w-4 text-black" />
        </div>
        <div className="flex flex-col">
          <span className="text-sm font-semibold text-zinc-100 leading-tight">Depot</span>
          <span className="text-[10px] text-zinc-500 leading-tight">Inventory Management</span>
        </div>
      </div>

      {/* Navigation */}
      <nav className="flex-1 overflow-y-auto py-4 custom-scrollbar">
        {menuGroups.map((group, idx) => (
          <div key={idx} className="mb-6">
            <h3 className="px-4 text-[10px] font-semibold text-zinc-600 tracking-wider mb-2">
              {group.title}
            </h3>
            <ul className="space-y-0.5">
              {group.items.map((item, itemIdx) => {
                const Icon = item.icon;
                return (
                  <li key={itemIdx}>
                    <Link 
                      href="#" 
                      className={`flex items-center gap-3 px-4 py-2 text-sm relative group
                        ${item.active 
                          ? 'text-zinc-100 bg-zinc-900/50' 
                          : 'text-zinc-400 hover:text-zinc-200 hover:bg-zinc-900/30'}`}
                    >
                      {item.active && (
                        <div className="absolute left-0 top-0 bottom-0 w-0.5 bg-yellow-600" />
                      )}
                      <Icon className="h-4 w-4" />
                      <span>{item.name}</span>
                      {item.badge && (
                        <span className="ml-auto text-[10px] font-medium bg-zinc-800 px-1.5 py-0.5 rounded text-zinc-300">
                          {item.badge}
                        </span>
                      )}
                    </Link>
                  </li>
                );
              })}
            </ul>
          </div>
        ))}
      </nav>

      {/* User Profile */}
      <div className="p-4 border-t border-zinc-900 flex items-center justify-between cursor-pointer hover:bg-zinc-900/50">
        <div className="flex items-center gap-3">
          <div className="h-8 w-8 rounded-full bg-pink-700 flex items-center justify-center text-xs font-medium">
            SC
          </div>
          <div className="flex flex-col">
            <span className="text-sm font-medium text-zinc-200 leading-tight">Sarah Chen</span>
            <span className="text-[11px] text-zinc-500 leading-tight">Admin</span>
          </div>
        </div>
        <ChevronDown className="h-4 w-4 text-zinc-500" />
      </div>
    </aside>
  );
};