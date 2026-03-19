import { Info } from 'lucide-react'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'

export default function FieldLabel({ children, hint }) {
  return (
    <label className="text-sm font-medium inline-flex items-center gap-1.5">
      {children}
      {hint && (
        <Tooltip>
          <TooltipTrigger type="button" tabIndex={-1}
            className="inline-flex items-center justify-center rounded-full text-muted-foreground hover:text-foreground transition-colors">
            <Info className="h-3.5 w-3.5" />
          </TooltipTrigger>
          <TooltipContent side="top" className="max-w-sm leading-relaxed">
            {hint}
          </TooltipContent>
        </Tooltip>
      )}
    </label>
  )
}
